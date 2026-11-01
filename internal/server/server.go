// Package server wires the HTTP API, auth, the page checks and the embedded SPA.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-search/internal/config"
	"github.com/abjelosevic88/pocket-homelab-search/internal/engine"
	"github.com/abjelosevic88/pocket-homelab-search/internal/instant"
	"github.com/abjelosevic88/pocket-homelab-search/internal/read"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	webFS    fs.FS
	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64
	engine   engine.Searcher
	fetch    *read.Fetcher
	online   *instant.Online // currency rates and weather for instant answers

	engMu    sync.Mutex
	engInfo  []engine.Info // SearXNG's engines, kept ten minutes
	engAt    time.Time
	resMu    sync.Mutex
	results  map[string]cachedSearch  // recent result pages per person
	inflight map[string]chan struct{} // searches on their way, so a second ask waits for the first
	modesMu  sync.Mutex

	oidcMu sync.Mutex
	oidc   *oidcProvider // for the issuer in use; rebuilt when it changes

	core    *core.Client
	coreMu  sync.Mutex
	coreOK  bool
	coreErr string
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	var eng engine.Searcher = engine.New(cfg.SearXNGURL, cfg.UserAgent, cfg.EngineTimeout)
	if cfg.SearXNGURL == "" && cfg.DemoData {
		eng = engine.Demo{}
	}
	return &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(), oidc: &oidcProvider{},
		engine: eng, fetch: read.NewFetcher(cfg.UserAgent, cfg.FetchPrivate), online: instant.NewOnline(cfg.UserAgent),
		core:    core.New(cfg.CoreURL, core.Options{DB: st.DB, TokenFile: cfg.DataDir + "/core.token", UserAgent: cfg.UserAgent}),
		results: map[string]cachedSearch{}, inflight: map[string]chan struct{}{}}
}

// Bootstrap seeds the first admin (if configured), the owner's integration
// settings from the environment and demo data, then starts the background work.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	if owner := s.store.OwnerID(ctx); owner > 0 {
		if err := s.ownerReady(ctx, owner); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	go s.janitor(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	return nil
}

// ownerReady runs once the box owner (the first admin) exists: the
// integration settings from the environment are the owner's — seeded into the
// owner's personal settings only, never into anyone else's — and demo data
// lands in the owner's account.
func (s *Server) ownerReady(ctx context.Context, owner int64) error {
	st, err := s.store.GetSettings(ctx, owner)
	if err != nil {
		return err
	}
	changed := false
	// Each value is copied once: clearing it in Settings must not bring it
	// back at the next start. A value already there counts as copied.
	once := func(name string, empty bool, v string, apply func()) {
		key := fmt.Sprintf("seeded:%d:%s", owner, name)
		if _, _, done := s.store.CacheGet(ctx, key); done {
			return
		}
		if empty && v != "" {
			apply()
			changed = true
			empty = false
		}
		if !empty {
			_ = s.store.CachePut(ctx, key, "1")
		}
	}
	set := func(name string, dst *string, v string) {
		once(name, *dst == "", v, func() { *dst = v })
	}
	set("OpenAIKey", &st.OpenAIKey, s.cfg.OpenAIKey)
	set("OpenAIModel", &st.OpenAIModel, s.cfg.OpenAIModel)
	set("OpenAIBaseURL", &st.OpenAIBaseURL, s.cfg.OpenAIBaseURL)
	if changed {
		if err := s.store.SaveUserSettings(ctx, owner, st); err != nil {
			return err
		}
	}
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx, owner); err != nil {
			return err
		}
	}
	return nil
}

// janitor forgets old history once an hour (each person's own keep_days).
func (s *Server) janitor(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		s.prune(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) prune(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	for _, uid := range ids {
		st, _ := s.store.GetSettings(ctx, uid)
		if n, err := s.store.PruneHistory(ctx, uid, st.HistoryDays); err == nil && n > 0 {
			s.log.Info("old history forgotten", "user", uid, "count", n)
		}
	}
}

// ---- Pocket Core ----

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh.
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
			s.reportModes(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

// eventTopics are what Search publishes on the Core bus: a result sent to
// another app (Pocket Flows can act on it). Searches themselves are never
// published.
var eventTopics = []string{"search.saved"}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "search", Name: "Pocket Search",
		Description: "Private web search: SearXNG asks ~70 search engines from your own server, your Pocket apps are searched alongside, and you decide which sites rise, sink or vanish. Bangs, a tracker-free reading view, AI answers with sources, and one tap sends a result to Reader, Notes, Tasks, Lookout or Wishlist.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "SEARCH",
		Publishes: eventTopics,
		Offers:    linkOffers,
		Uses:      append(linkUses(), core.InferenceUse()),
	}
	if s.cfg.BaseURL != "" {
		// ask Core for a relying-party client so "Sign in with Pocket Core" needs no configuration
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.oidcCallbackURL()}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}

// publish sends an event for a person (fire and forget, through the outbox).
func (s *Server) publish(ctx context.Context, uid int64, topic, subject string, payload map[string]any) {
	if !s.core.Configured() {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		payload["user_email"] = u.Email
	}
	if err := s.core.Publish(ctx, topic, subject, payload); err != nil {
		s.log.Debug("pocket core: publish failed", "topic", topic, "err", err)
	}
}
