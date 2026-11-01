package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-search/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-search/internal/engine"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with keys masked.
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	apps := map[string]any{}
	for slug, c := range st.Apps {
		apps[slug] = map[string]any{"mode": c.Mode, "url": c.URL, "token": mask(c.Token), "public": c.Public, "search": c.Search == nil || *c.Search}
	}
	off, on := st.EnginesOff, st.EnginesOn
	if off == nil {
		off = []string{}
	}
	if on == nil {
		on = []string{}
	}
	return map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat,
		"safe_search": st.SafeSearch, "language": st.Language, "default_category": st.DefaultCategory, "new_tab": st.NewTab,
		"suggestions": st.Suggestions, "reader_images": st.ReaderImages, "engines_off": off, "engines_on": on,
		"history": st.History, "history_days": st.HistoryDays, "apps_in_results": st.AppsInResults, "apps": apps,
		"ai_answers": st.AIAnswers, "ai_read_pages": st.AIReadPages, "ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel,
		"openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"engine": s.engineName(), "engine_ready": s.engine.Configured(), "fetch_private": s.cfg.FetchPrivate,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin and silently ignored for everyone else.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	intIn := func(k string, dst *int, v, lo, hi int) {
		if keys[k] && v >= lo && v <= hi {
			*dst = v
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	oneOf := func(k string, dst *string, v string, allowed ...string) {
		if !keys[k] {
			return
		}
		for _, a := range allowed {
			if v == a {
				*dst = v
			}
		}
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	text("date_format", &cur.DateFormat, in.DateFormat)
	text("time_format", &cur.TimeFormat, in.TimeFormat)
	intIn("safe_search", &cur.SafeSearch, in.SafeSearch, 0, 2)
	if keys["language"] {
		l := strings.TrimSpace(in.Language)
		if l == "" {
			l = "auto"
		}
		if len(l) > 12 || strings.ContainsAny(l, " /?&") {
			writeErr(w, http.StatusBadRequest, "unknown language")
			return
		}
		cur.Language = l
	}
	oneOf("default_category", &cur.DefaultCategory, in.DefaultCategory, "general", "images", "videos", "news", "apps")
	boolIn("new_tab", &cur.NewTab, in.NewTab)
	boolIn("suggestions", &cur.Suggestions, in.Suggestions)
	boolIn("reader_images", &cur.ReaderImages, in.ReaderImages)
	if keys["engines_off"] {
		cur.EnginesOff = cleanNames(in.EnginesOff)
	}
	if keys["engines_on"] {
		cur.EnginesOn = cleanNames(in.EnginesOn)
	}
	boolIn("history", &cur.History, in.History)
	intIn("history_days", &cur.HistoryDays, in.HistoryDays, 0, 3650)
	boolIn("apps_in_results", &cur.AppsInResults, in.AppsInResults)
	appsChanged := false
	if keys["apps"] {
		if cur.Apps == nil {
			cur.Apps = map[string]store.AppConn{}
		}
		for slug, c := range in.Apps {
			if _, ok := appBySlug(slug); !ok {
				continue
			}
			old := cur.Apps[slug]
			switch c.Mode {
			case "", modeCore, modeDirect, modeOff:
				old.Mode = c.Mode
			}
			old.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
			old.Public = strings.TrimRight(strings.TrimSpace(c.Public), "/")
			if !strings.Contains(c.Token, "••") {
				old.Token = strings.TrimSpace(c.Token)
			}
			if c.Search != nil {
				v := *c.Search
				old.Search = &v
			}
			cur.Apps[slug] = old
			appsChanged = true
		}
	}
	oneOf("ai_answers", &cur.AIAnswers, in.AIAnswers, "off", "ask", "auto")
	boolIn("ai_read_pages", &cur.AIReadPages, in.AIReadPages)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			writeErr(w, http.StatusBadRequest, "AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			writeErr(w, http.StatusBadRequest, "unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	if keys["openai_base_url"] {
		cur.OpenAIBaseURL = strings.TrimRight(strings.TrimSpace(in.OpenAIBaseURL), "/")
	}
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	if appsChanged {
		s.reportModesSoon()
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

func cleanNames(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n != "" && len(n) < 80 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return in.URL
	}
	var msg string
	var err error
	switch in.Which {
	case "openai":
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	case "engine":
		start := time.Now()
		res, e := s.engine.Search(ctx, engine.Query{Q: "pocket search test", Category: "general", SafeSearch: 1})
		if e != nil {
			err = e
			break
		}
		msg = fmt.Sprintf("The search engine works (%d results in %d ms)", len(res.Results), time.Since(start).Milliseconds())
		if len(res.Unresponsive) > 0 {
			msg += fmt.Sprintf("; %d engines did not answer this time", len(res.Unresponsive))
		}
	default:
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// ---- system / backup ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "searches_run": searchesRun.Load(),
		"engine": s.engineName(), "engine_ready": s.engine.Configured(), "engine_url": s.cfg.SearXNGURL, "fetch_private": s.cfg.FetchPrivate,
	})
}

// handleBackup downloads the whole database, so it is a box operation for an
// admin only (everyone can export their own data as JSON).
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-search-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}
