package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-search/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
	"github.com/go-chi/chi/v5"
)

// ---- your Pocket apps: searched next to the web, and where results go ----
//
// Each person chooses how Search reaches each app: through a Pocket Core app
// link (Search asks Core for a five-minute token acting as that person,
// nothing to paste), directly with their own URL + API token, or not at all.
// Core tokens are asked for per request with only the scopes the work needs
// and never kept, so a link switched off in Core applies at once. Searching
// goes through the search capability (capable.go); the apps a result can be
// sent to, or an instant answer read from, are described in apps.go.

// appSend says how to send a result to one app.
type appSend struct {
	scope string
	label string // "Save to Reader"
	done  string // "Saved to Reader"
	path  string
	body  func(in sendIn) map[string]any
	id    []string // where the new item's id is in the answer
	route string   // the new item's page: "/articles/{id}"
}

// pocketApp is one sibling Pocket app Search acts in.
type pocketApp struct {
	slug, name string
	reason     string
	answer     string // the scope an instant answer reads with ("tasks:read"); "" = none
	send       *appSend
}

func (a pocketApp) scopes() []string {
	var out []string
	if a.answer != "" {
		out = append(out, a.answer)
	}
	if a.send != nil && a.send.scope != a.answer {
		out = append(out, a.send.scope)
	}
	return out
}

func appBySlug(slug string) (pocketApp, bool) {
	for _, a := range pocketApps {
		if a.slug == slug {
			return a, true
		}
	}
	return pocketApp{}, false
}

// linkUses is the manifest's Uses: the table's apps one by one, and the
// search capability in every app that offers it (capable.go).
func linkUses() []core.Use {
	out := make([]core.Use, 0, len(pocketApps)+1)
	for _, a := range pocketApps {
		out = append(out, core.Use{App: a.slug, Scopes: a.scopes(), Reason: a.reason})
	}
	return append(out, core.UseEvery(core.ScopeSearch, "Find your things in every app that can be searched, beside the web"))
}

// Connection modes, chosen per person and per app in Settings.
const (
	modeCore   = "core"
	modeDirect = "direct"
	modeOff    = "off"
)

// connMode is the mode in effect: the chosen one, else direct when a token
// was entered, else Pocket Core when Search is connected to one, else direct.
func (s *Server) connMode(mode, token string) string {
	switch mode {
	case modeCore, modeDirect, modeOff:
		return mode
	}
	if token != "" || !s.core.Configured() {
		return modeDirect
	}
	return modeCore
}

// notConnected means the person has no working connection to the app.
type notConnected struct{ msg string }

func (e *notConnected) Error() string { return e.msg }

func isNotConnected(err error) bool {
	var nc *notConnected
	return errors.As(err, &nc)
}

// siblingConn is how Search reaches one person's copy of a sibling app.
type siblingConn struct {
	client *pocket.Client
	via    string
	public string
}

func linkReason(app, reason string) string {
	switch reason {
	case "off_person":
		return "you switched the Search → " + app + " link off in Pocket Core (Settings → Apps acting for me)"
	case "off_admin":
		return "the Search → " + app + " link is switched off in Pocket Core (Links)"
	case "target_missing":
		return app + " is not registered with Pocket Core"
	case "not_offered":
		return "this " + app + " version does not accept app links yet"
	case "not_declared":
		return "Pocket Core has not seen this Search version yet; it re-registers within a minute of starting"
	case "not_in_audience":
		return "you do not have access to " + app + " in Pocket Core"
	case "unknown_user":
		return "your email is not a person in Pocket Core"
	}
	return ""
}

// siblingFor connects to app a for person u with exactly these scopes.
func (s *Server) siblingFor(ctx context.Context, u *store.User, st store.Settings, a pocketApp, scopes ...string) (*siblingConn, error) {
	if u == nil {
		return nil, &notConnected{"nobody to act for"}
	}
	c := st.Apps[a.slug]
	switch s.connMode(c.Mode, c.Token) {
	case modeOff:
		return nil, &notConnected{a.name + " is switched off in Settings → Connected apps"}
	case modeDirect:
		if c.Token == "" {
			return nil, &notConnected{a.name + " is not set up (Settings → Connected apps): add its address and a token, or use Pocket Core"}
		}
		if c.URL == "" {
			return nil, &notConnected{"set the " + a.name + " address in Settings → Connected apps"}
		}
		return &siblingConn{client: pocket.New(c.URL, c.Token, 10*time.Second), via: modeDirect, public: firstNonEmpty(c.Public, c.URL)}, nil
	}
	if !s.core.Configured() {
		return nil, &notConnected{a.name + " through Pocket Core: there is no Pocket Core on this box; use a direct connection"}
	}
	tok, err := s.core.ActAs(ctx, u.Email, a.slug, scopes...)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			if t := linkReason(a.name, le.Reason); t != "" {
				return nil, &notConnected{a.name + " is not set up: " + t}
			}
			return nil, &notConnected{a.name + " is not set up: " + le.Error()}
		}
		return nil, fmt.Errorf("%s through Pocket Core: %w", a.name, err)
	}
	if tok.URL == "" || tok.AccessToken == "" {
		return nil, fmt.Errorf("%s through Pocket Core: Core gave no address or token", a.name)
	}
	return &siblingConn{client: pocket.New(tok.URL, tok.AccessToken, 10*time.Second), via: modeCore, public: firstNonEmpty(c.Public, firstNonEmpty(tok.PublicURL, tok.URL))}, nil
}

// connStatus says, without issuing anything, how a person reaches an app:
// its own scopes and, when it offers the search capability (cap), that link
// too.
func (s *Server) connStatus(ctx context.Context, u *store.User, st store.Settings, a pocketApp, cap *core.CapableApp) (eff, via, detail string) {
	c := st.Apps[a.slug]
	eff = s.connMode(c.Mode, c.Token)
	switch eff {
	case modeOff:
		return eff, "", "switched off"
	case modeDirect:
		if c.Token == "" {
			return eff, "", "no token yet"
		}
		return eff, modeDirect, ""
	}
	if u == nil || !s.core.Configured() {
		return eff, "", "there is no Pocket Core on this box"
	}
	list, err := s.core.LinkStatus(ctx, u.Email, a.slug)
	if err != nil {
		return eff, "", "Pocket Core did not answer: " + err.Error()
	}
	state := map[string]core.LinkState{}
	for _, l := range list {
		state[l.Scope] = l
	}
	if cap != nil {
		state[core.ScopeSearch] = core.LinkState{Scope: core.ScopeSearch, On: cap.On, State: cap.State}
	}
	scopes := a.scopes()
	if cap != nil {
		scopes = append(scopes, core.ScopeSearch)
	}
	var on, off []string
	reason := ""
	for _, sc := range scopes {
		l, ok := state[sc]
		if ok && l.On {
			on = append(on, sc)
			continue
		}
		off = append(off, sc)
		if reason == "" {
			reason = "not_declared"
			if ok && l.State != "" {
				reason = l.State
			}
		}
	}
	text := linkReason(a.name, reason)
	if text == "" {
		text = reason
	}
	switch {
	case len(off) == 0:
		return eff, modeCore, ""
	case len(on) == 0:
		return eff, "", text
	}
	sort.Strings(off)
	return eff, modeCore, "partly: " + strings.Join(off, ", ") + " off (" + text + ")"
}

// ---- reading another app's answer ----

func pick(row map[string]any, paths []string) string {
	for _, p := range paths {
		var v any = row
		for _, k := range strings.Split(p, ".") {
			m, ok := v.(map[string]any)
			if !ok {
				v = nil
				break
			}
			v = m[k]
		}
		switch x := v.(type) {
		case string:
			if strings.TrimSpace(x) != "" {
				return x
			}
		case float64:
			if x != 0 {
				return fmt.Sprintf("%.0f", x)
			}
		case bool:
			return fmt.Sprint(x)
		}
	}
	return ""
}

// appItem is one match in a Pocket app.
type appItem struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Date    string `json:"date,omitempty"`
	URL     string `json:"url"`            // the item in the app
	Link    string `json:"link,omitempty"` // an outside address it carries
	Kind    string `json:"kind"`
	Strong  bool   `json:"strong,omitempty"` // its title holds every word searched: shown among the web results
}

// appSection is one app's matches.
type appSection struct {
	App   string    `json:"app"`
	Name  string    `json:"name"`
	Icon  string    `json:"icon,omitempty"` // the app's own icon, when Pocket Core names the app
	Items []appItem `json:"items"`
	Error string    `json:"error,omitempty"`
	Off   bool      `json:"off,omitempty"` // not connected (a hint, not an error)
	More  string    `json:"more,omitempty"`
	MS    int64     `json:"ms"`
}

var reRouteField = regexp.MustCompile(`\{([a-z_.]+)\}`)

// itemURL is an item's page in the app: route with {id} (the item's id, from
// idPaths) and any other {field} filled from the row; the app's start page
// when a part is missing.
func itemURL(public, route string, row map[string]any, idPaths []string) string {
	public = strings.TrimRight(public, "/")
	if route == "" || row == nil {
		return public + "/"
	}
	missing := false
	out := reRouteField.ReplaceAllStringFunc(route, func(m string) string {
		name := m[1 : len(m)-1]
		v := ""
		if name == "id" {
			v = pick(row, idPaths)
		} else {
			v = pick(row, []string{name})
		}
		if v == "" {
			missing = true
		}
		return url.PathEscape(v)
	})
	if missing {
		return public + "/"
	}
	return public + out
}

// searchDirect searches an app the person reaches directly, with their own
// address and token, on the search capability's route: the same answer Core
// would pass on, without Core.
func (s *Server) searchDirect(ctx context.Context, u *store.User, st store.Settings, a pocketApp, q string, limit int) appSection {
	sec := appSection{App: a.slug, Name: a.name, Items: []appItem{}}
	start := time.Now()
	defer func() { sec.MS = time.Since(start).Milliseconds() }()
	sib, err := s.siblingFor(ctx, u, st, a, core.ScopeSearch)
	if err != nil {
		sec.Error, sec.Off = err.Error(), isNotConnected(err)
		return sec
	}
	var res core.SearchResults
	if err := sib.client.Get(ctx, core.SearchPath, url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}, &res); err != nil {
		sec.Error = a.name + " did not answer: " + err.Error()
		return sec
	}
	public := strings.TrimRight(sib.public, "/")
	for i, it := range res.Items {
		if strings.HasPrefix(it.URL, "/") {
			res.Items[i].URL = public + it.URL
		}
	}
	sec.Items = appItems(res.Items, public, "", "", q, limit)
	sec.More = public + "/"
	return sec
}

// strongMatch: the title holds every word of the search (at least three
// letters searched), like a note named exactly what you looked for.
func strongMatch(title, q string) bool {
	words := strings.Fields(strings.ToLower(q))
	if len(strings.Join(words, "")) < 3 {
		return false
	}
	t := strings.ToLower(title)
	for _, w := range words {
		if !strings.Contains(t, w) {
			return false
		}
	}
	return true
}

var markup = strings.NewReplacer("**", "", "__", "", "##", "", "# ", "", "`", "", "<mark>", "", "</mark>", "", "<b>", "", "</b>", "")

func stripMarkup(s string) string { return markup.Replace(s) }

// cleanSnippet is a match's line of context as shown: no markup, one line.
// Pocket Files marks matched words with \x01…\x02.
func cleanSnippet(s string) string {
	return oneLine(stripMarkup(strings.NewReplacer("\x01", "", "\x02", "").Replace(s)), 220)
}

// handleSearchApps: GET /api/v1/search/apps?q=&app= — the person's Pocket
// apps, searched at the same time (each bounded to a few seconds): every app
// that offers the search capability, and the table's apps the person reaches
// directly.
func (s *Server) handleSearchApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 200 {
		writeErr(w, http.StatusBadRequest, "type up to 200 characters to search your apps")
		return
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	only := r.URL.Query().Get("app")
	limit := 4
	if only != "" || r.URL.Query().Get("all") == "1" {
		limit = 20
	}
	if s.cfg.DemoData && s.engineName() == "demo" && !s.core.Configured() {
		writeJSON(w, http.StatusOK, map[string]any{"query": q, "sections": demoSections(q, only, limit)})
		return
	}
	// the apps that offer the search capability and the ones reached directly (capable.go)
	apps := s.searchTargets(ctx, u, st, only)
	out := make([]appSection, len(apps))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, a := range apps {
		wg.Add(1)
		go func(i int, a searchTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			actx, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()
			out[i] = s.searchOne(actx, u, st, a, q, limit)
		}(i, a)
	}
	wg.Wait()
	// apps with matches first, then the rest in their usual order
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].Items) > 0 && len(out[j].Items) == 0 })
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "sections": out})
}

// handleApps lists the Pocket apps Search can use and how the person is
// connected: the table's apps (with a direct connection), and every other app
// that offers the search capability, all by name.
func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, _ := s.store.GetSettings(ctx, u.ID)
	type row struct {
		Slug    string   `json:"slug"`
		Name    string   `json:"name"`
		Reason  string   `json:"reason"`
		Scopes  []string `json:"scopes"`
		Search  bool     `json:"can_search"`
		Send    string   `json:"send,omitempty"` // the action label
		Bang    string   `json:"bang,omitempty"`
		Mode    string   `json:"mode"` // the mode in effect
		Via     string   `json:"via"`  // core | direct | "" (not connected)
		Detail  string   `json:"detail"`
		InSerps bool     `json:"in_results"`
		Icon    string   `json:"icon"`       // the app's own icon (an address), for an app Pocket Core names
		Found   bool     `json:"discovered"` // found through Pocket Core on its own: no direct connection
	}
	capable := s.capableApps(ctx, u)
	bySlug := capableBySlug(capable)
	bangs := capBangs(capable)
	out := make([]row, len(pocketApps))
	var wg sync.WaitGroup
	for i, a := range pocketApps {
		wg.Add(1)
		go func(i int, a pocketApp) {
			defer wg.Done()
			var cp *core.CapableApp
			if ca, ok := bySlug[a.slug]; ok {
				cp = &ca
			}
			eff, via, detail := s.connStatus(ctx, u, st, a, cp)
			c := st.Apps[a.slug]
			rw := row{Slug: a.slug, Name: a.name, Reason: a.reason, Scopes: a.scopes(), Mode: eff, Via: via, Detail: detail}
			// searched through Pocket Core when it offers the capability, or on
			// its own route when the person reaches it directly
			rw.Search = cp != nil || eff == modeDirect
			if cp != nil {
				rw.Reason = strings.TrimSuffix(capReason(*cp), ".") + ". " + a.reason
				rw.Scopes = append(rw.Scopes, core.ScopeSearch)
				rw.Icon = cp.Icon
				if b := bangs[a.slug]; len(b) > 0 {
					rw.Bang = b[0]
				}
			} else if eff == modeDirect {
				rw.Bang = a.slug
			}
			rw.InSerps = rw.Search && (c.Search == nil || *c.Search)
			if a.send != nil {
				rw.Send = a.send.label
			}
			out[i] = rw
		}(i, a)
	}
	wg.Wait()
	// the apps that offer the search capability and are not in the table
	for _, a := range capable {
		if _, ok := appBySlug(a.App); ok {
			continue
		}
		c := st.Apps[a.App]
		name := capName(a)
		rw := row{Slug: a.App, Name: name, Reason: capReason(a), Scopes: []string{core.ScopeSearch}, Search: true,
			Mode: discoveredMode(c), InSerps: c.Search == nil || *c.Search, Icon: a.Icon, Found: true}
		if b := bangs[a.App]; len(b) > 0 {
			rw.Bang = b[0]
		}
		switch {
		case rw.Mode == modeOff:
			rw.Detail = "switched off"
		case a.On:
			rw.Via = modeCore
		default:
			rw.Detail = capOffText(name, a.State, a.Detail)
		}
		out = append(out, rw)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	writeJSON(w, http.StatusOK, out)
}

// handleDisconnectApp forgets a direct token for an app.
func (s *Server) handleDisconnectApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := userFrom(ctx)
	slug := chi.URLParam(r, "slug")
	if _, ok := appBySlug(slug); !ok {
		writeErr(w, http.StatusNotFound, "unknown app")
		return
	}
	st, _ := s.store.GetSettings(ctx, me.ID)
	c := st.Apps[slug]
	c.Token, c.URL, c.Mode = "", "", ""
	if st.Apps == nil {
		st.Apps = map[string]store.AppConn{}
	}
	st.Apps[slug] = c
	if err := s.store.SaveUserSettings(ctx, me.ID, st); err != nil {
		s.fail(w, err, "settings")
		return
	}
	s.reportModesSoon()
	w.WriteHeader(http.StatusNoContent)
}

// ---- sending a result to an app ----

type sendIn struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Query   string `json:"query"`
	Note    string `json:"note"` // the person's own words (optional)
}

// handleSend: POST /api/v1/send/{target} — a result to Reader, Notes, Tasks…
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	a, ok := appBySlug(chi.URLParam(r, "target"))
	if !ok || a.send == nil {
		writeErr(w, http.StatusNotFound, "results cannot be sent there")
		return
	}
	var in sendIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.URL, in.Title = strings.TrimSpace(in.URL), oneLine(in.Title, 300)
	if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		writeErr(w, http.StatusBadRequest, "a result needs its web address")
		return
	}
	if in.Title == "" {
		in.Title = in.URL
	}
	in.Snippet = oneLine(in.Snippet, 600)
	st, _ := s.store.GetSettings(ctx, u.ID)
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var itemLink string
	if s.cfg.DemoData && s.engineName() == "demo" && !s.core.Configured() {
		itemLink = "#"
	} else {
		sib, err := s.siblingFor(sctx, u, st, a, a.send.scope)
		if err != nil {
			status := http.StatusBadGateway
			if isNotConnected(err) {
				status = http.StatusConflict
			}
			writeErr(w, status, err.Error())
			return
		}
		var res map[string]any
		if err := sib.client.Post(sctx, a.send.path, a.send.body(in), &res); err != nil {
			writeErr(w, http.StatusBadGateway, a.name+": "+err.Error())
			return
		}
		itemLink = itemURL(sib.public, a.send.route, res, a.send.id)
	}
	v := &store.Save{URL: in.URL, App: a.slug, Title: in.Title, ItemURL: itemLink}
	if err := s.store.AddSave(ctx, u.ID, v); err != nil {
		s.fail(w, err, "save")
		return
	}
	s.publish(ctx, u.ID, "search.saved", in.Title, map[string]any{"app": a.slug, "url": in.URL, "title": in.Title, "query": in.Query, "item_url": itemLink})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": a.send.done, "item_url": itemLink, "save": v})
}
