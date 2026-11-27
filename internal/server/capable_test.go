package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-search/internal/config"
)

// capApp is a fake Pocket app behind the fake Core: it offers the search
// capability and remembers what it was asked.
type capApp struct {
	slug, name, icon, public string
	search                   *core.Searchable
	state                    string            // how the pocket:search link stands ("on" = it works)
	stateFor                 map[string]string // another state for one person (by email)
	legacy                   map[string]bool   // the table's own scopes (notes:read…) and whether their link is on
	answer                   func(w http.ResponseWriter, r *http.Request)

	srv   *httptest.Server
	mu    sync.Mutex
	paths []string // every path asked for
	auth  string   // the last Authorization
	query url.Values
}

func (c *capApp) asked() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.paths...)
}

// last is how it was asked the last time.
func (c *capApp) last() (auth string, query url.Values) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.auth, c.query
}

func (c *capApp) reset() {
	c.mu.Lock()
	c.paths = nil
	c.mu.Unlock()
}

// capCore is a fake Pocket Core that knows capabilities: it lists who offers
// pocket:search (GET /links/status?scope=) and hands out link tokens.
type capCore struct {
	srv  *httptest.Server
	mu   sync.Mutex
	apps map[string]*capApp
	// an older Core: it has never heard of ?scope= or of "every app", so it
	// answers with the table's links only and says nothing about the apps
	old      bool
	down     bool           // the list cannot be read (HTTP 500)
	asked    int            // how often the list of searchable apps was read
	manifest *core.Manifest // what Search registered with
}

func (f *capCore) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

// count is how often the list of searchable apps was read.
func (f *capCore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked
}

func (f *capCore) stateOf(c *capApp, email string) string {
	if st, ok := c.stateFor[email]; ok {
		return st
	}
	return c.state
}

func newCapCore(t *testing.T, apps ...*capApp) *capCore {
	t.Helper()
	f := &capCore{apps: map[string]*capApp{}}
	for _, c := range apps {
		c := c
		if c.state == "" {
			c.state = "on"
		}
		c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.mu.Lock()
			c.paths = append(c.paths, r.URL.Path)
			c.auth, c.query = r.Header.Get("Authorization"), r.URL.Query()
			c.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			c.answer(w, r)
		}))
		t.Cleanup(c.srv.Close)
		f.apps[c.slug] = c
	}
	refuse := func(w http.ResponseWriter, reason string) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "the link does not work: " + reason, "reason": reason})
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m core.Manifest
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.manifest = &m
			_, _ = io.WriteString(w, `{}`)
		case "/api/v1/links/status":
			q := r.URL.Query()
			email, scope, only := q.Get("user"), q.Get("scope"), q.Get("app")
			if scope != "" {
				f.asked++
				if f.down {
					http.Error(w, `{"error":"core is restarting"}`, http.StatusInternalServerError)
					return
				}
			}
			slugs := make([]string, 0, len(f.apps))
			for slug := range f.apps {
				slugs = append(slugs, slug)
			}
			sort.Strings(slugs)
			rows := []map[string]any{}
			for _, slug := range slugs {
				c := f.apps[slug]
				if only != "" && only != slug {
					continue
				}
				if !f.old && (scope == "" || scope == core.ScopeSearch) {
					st := f.stateOf(c, email)
					row := map[string]any{"app": slug, "scope": core.ScopeSearch, "state": st, "on": st == "on", "detail": "the link is " + st}
					if scope != "" {
						row["target"] = map[string]any{"name": c.name, "icon": c.icon, "url": c.srv.URL, "public_url": c.public, "version": "1.0.0", "category": "Everyday", "search": c.search}
					}
					rows = append(rows, row)
				}
				if scope == "" || f.old {
					for sc, on := range c.legacy {
						st := "off_admin"
						if on {
							st = "on"
						}
						rows = append(rows, map[string]any{"app": slug, "scope": sc, "state": st, "on": on})
					}
				}
			}
			_ = json.NewEncoder(w).Encode(rows)
		case "/api/v1/links/token":
			var in struct {
				User   string   `json:"user"`
				App    string   `json:"app"`
				Scopes []string `json:"scopes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			c := f.apps[in.App]
			if c == nil {
				refuse(w, "target_missing")
				return
			}
			for _, sc := range in.Scopes {
				switch {
				case sc == core.ScopeSearch && f.old:
					refuse(w, "not_declared")
					return
				case sc == core.ScopeSearch && f.stateOf(c, in.User) != "on":
					refuse(w, f.stateOf(c, in.User))
					return
				case sc != core.ScopeSearch && !c.legacy[sc]:
					refuse(w, "off_admin")
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_" + in.App + "_" + strings.Join(in.Scopes, "+") + "_" + in.User, "url": c.srv.URL, "public_url": c.public})
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// capBox starts Search connected to the fake Core.
func capBox(t *testing.T, f *capCore) *app {
	t.Helper()
	return newApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = f.srv.URL, "prk_test" })
}

// forget empties the half-minute memory of who can be searched.
func (a *app) forgetCapable() {
	a.s.capMu.Lock()
	a.s.capable = map[string]capableEntry{}
	a.s.capMu.Unlock()
}

func sections(m map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, s := range m["sections"].([]any) {
		sec := s.(map[string]any)
		out[sec["app"].(string)] = sec
	}
	return out
}

// recipesApp is an app Search has never heard of: no entry in apps.go.
func recipesApp() *capApp {
	return &capApp{slug: "recipes", name: "Pocket Recipes", icon: "https://recipes.example/favicon.svg", public: "https://recipes.example",
		search: &core.Searchable{What: "Your recipes, by name and ingredient", Kinds: []string{"recipe"}, Bangs: []string{"recipes", "cook", "w", "notes", "Bad!", "x"}},
		answer: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != core.SearchPath {
				http.NotFound(w, r)
				return
			}
			switch r.URL.Query().Get("q") {
			case "scored":
				_, _ = io.WriteString(w, `{"items":[{"id":"1","title":"Low","score":1},{"id":"2","title":"High","score":9.5},{"id":"3","title":"Middle","score":4},{"id":"4","title":"Also middle","score":4}]}`)
			default:
				_, _ = io.WriteString(w, `{"items":[
					{"id":"1","title":"Sourdough bread","snippet":"**feed** the <mark>starter</mark>\n twice","kind":"loaf","date":"2026-11-20T08:12:00Z","url":"/r/1"},
					{"id":"2","title":"Rye loaf","url":"https://elsewhere.example/rye","link":"https://blog.example/rye"},
					{"id":"3","title":"Bread basics"},
					{"id":"4","title":"","link":"https://blog.example/untitled"},
					{"id":"5","title":""}]}`)
			}
		}}
}

// An app that says in its manifest that it can be searched shows up beside
// the web with nothing added to Search: found through Core, searched on the
// contract's route as the person.
func TestDiscoveredAppIsSearched(t *testing.T) {
	rec := recipesApp()
	f := newCapCore(t, rec)
	a := capBox(t, f)
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q="+url.QueryEscape("sourdough bread")+"&app=recipes", nil)
	secs := m["sections"].([]any)
	if len(secs) != 1 {
		t.Fatalf("sections = %v", secs)
	}
	sec := secs[0].(map[string]any)
	if sec["app"] != "recipes" || sec["name"] != "Pocket Recipes" || sec["icon"] != "https://recipes.example/favicon.svg" || sec["more"] != "https://recipes.example/" || sec["error"] != nil || sec["off"] != nil {
		t.Fatalf("section = %v", sec)
	}
	auth, query := rec.last()
	if got := rec.asked(); len(got) != 1 || got[0] != core.SearchPath || auth != "Bearer pcx_recipes_pocket:search_alice@example.com" || query.Get("q") != "sourdough bread" || query.Get("limit") != "20" {
		t.Fatalf("the app was asked %v as %q with %v", got, auth, query)
	}
	items := sec["items"].([]any)
	want := []map[string]any{
		// the item's own kind, its page made absolute, the snippet cleaned, the title holds every word
		{"title": "Sourdough bread", "snippet": "feed the starter twice", "kind": "loaf", "date": "2026-11-20T08:12:00Z", "url": "https://recipes.example/r/1", "strong": true},
		// an address elsewhere is kept; no kind: the one kind the app has
		{"title": "Rye loaf", "snippet": "", "kind": "recipe", "url": "https://elsewhere.example/rye", "link": "https://blog.example/rye"},
		// no page: the app itself
		{"title": "Bread basics", "snippet": "", "kind": "recipe", "url": "https://recipes.example/"},
		// no title: the outside address stands in; nothing at all: left out
		{"title": "https://blog.example/untitled", "snippet": "", "kind": "recipe", "url": "https://recipes.example/", "link": "https://blog.example/untitled"},
	}
	if len(items) != len(want) {
		t.Fatalf("items = %v", items)
	}
	for i, w := range want {
		if got := items[i].(map[string]any); fmt.Sprint(got) != fmt.Sprint(w) {
			t.Errorf("item %d = %v, want %v", i, got, w)
		}
	}
	// beside the web it asks for four, like every other app, and the apps
	// that are not connected do not hold it back
	m = a.ok(a.alice, "GET", "/api/v1/search/apps?q=bread", nil)
	all := m["sections"].([]any)
	_, query = rec.last()
	if first := all[0].(map[string]any); first["app"] != "recipes" || len(first["items"].([]any)) != 4 || query.Get("limit") != "4" {
		t.Fatalf("first section = %v (limit %q)", first, query.Get("limit"))
	}
	searchable := 0
	for _, p := range pocketApps {
		if p.search != nil {
			searchable++
		}
	}
	if len(all) != searchable+1 {
		t.Fatalf("%d sections, want the table's %d and Recipes", len(all), searchable)
	}
	// an app that scores its matches decides their order; equal scores keep theirs
	m = a.ok(a.alice, "GET", "/api/v1/search/apps?q=scored&app=recipes", nil)
	var titles []string
	for _, it := range sections(m)["recipes"]["items"].([]any) {
		titles = append(titles, it.(map[string]any)["title"].(string))
	}
	if strings.Join(titles, ",") != "High,Middle,Also middle,Low" {
		t.Fatalf("scored order = %v", titles)
	}
	// who can be searched is asked once in a while, not on every search
	if n := f.count(); n != 1 {
		t.Fatalf("Core was asked %d times who can be searched", n)
	}
	// the manifest asks for the capability once, for every app
	deadline := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		man := f.manifest
		f.mu.Unlock()
		if man != nil {
			last := man.Uses[len(man.Uses)-1]
			if last.App != core.InferenceUse().App {
				t.Fatalf("the last use is %+v, want Pocket AI's", last)
			}
			every := man.Uses[len(man.Uses)-2]
			if every.App != core.EveryApp || len(every.Scopes) != 1 || every.Scopes[0] != core.ScopeSearch || every.Reason == "" || len(man.Uses) != len(pocketApps)+2 {
				t.Fatalf("uses end with %+v (%d of them)", every, len(man.Uses))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Search never registered with Core")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Pocket Notes is in the table and has adopted the contract: with the
// capability's link on it is searched through the contract, with it off (or
// not approved yet) through the table's adapter as before, and a direct
// connection never goes through Core.
func TestTableAppWithTheCapability(t *testing.T) {
	answer := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case core.SearchPath:
			_, _ = io.WriteString(w, `{"items":[{"id":"3","title":"Sourdough notes","snippet":"through the contract","kind":"note","url":"/note/3"}]}`)
		case "/api/v1/search":
			_, _ = io.WriteString(w, `[{"id":3,"title":"Sourdough notes","snippet":"the table's way"}]`)
		default:
			http.NotFound(w, r)
		}
	}
	notes := &capApp{slug: "notes", name: "Notes (as Core calls it)", icon: "https://notes.example/favicon.svg", public: "https://notes.example",
		search: &core.Searchable{What: "Your notes", Kinds: []string{"note"}, Bangs: []string{"notes", "jot"}}, legacy: map[string]bool{"notes:read": true}, answer: answer}
	f := newCapCore(t, notes)
	a := capBox(t, f)
	find := func(auth string) map[string]any {
		t.Helper()
		notes.reset()
		m := a.ok(auth, "GET", "/api/v1/search/apps?q=sourdough&app=notes", nil)
		secs := m["sections"].([]any)
		if len(secs) != 1 {
			t.Fatalf("sections = %v", secs)
		}
		return secs[0].(map[string]any)
	}
	snippet := func(sec map[string]any) string {
		t.Helper()
		items := sec["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["url"] != "https://notes.example/note/3" {
			t.Fatalf("section = %v", sec)
		}
		return items[0].(map[string]any)["snippet"].(string)
	}
	// on: the contract, under the table's name and without another icon
	sec := find(a.alice)
	if got := notes.asked(); snippet(sec) != "through the contract" || len(got) != 1 || got[0] != core.SearchPath || sec["name"] != "Pocket Notes" || sec["icon"] != nil {
		t.Fatalf("on: section %v, asked %v", sec, got)
	}
	// switched off a moment ago, while Search still believes it is on: Core
	// refuses the token and the table's adapter answers in the same search
	f.set(func() { notes.state = "off_person" })
	sec = find(a.alice)
	if got := notes.asked(); snippet(sec) != "the table's way" || len(got) != 1 || got[0] != "/api/v1/search" {
		t.Fatalf("just switched off: section %v, asked %v", sec, got)
	}
	// off and known to be off: the table's adapter straight away
	sec = find(a.alice)
	auth, _ := notes.last()
	if got := notes.asked(); snippet(sec) != "the table's way" || len(got) != 1 || got[0] != "/api/v1/search" || auth != "Bearer pcx_notes_notes:read_alice@example.com" {
		t.Fatalf("off: section %v, asked %v as %q", sec, got, auth)
	}
	// the table's own link off as well: not connected, as before
	f.set(func() { notes.legacy["notes:read"] = false })
	if sec = find(a.alice); sec["off"] != true || len(notes.asked()) != 0 {
		t.Fatalf("both off: section %v, asked %v", sec, notes.asked())
	}
	// the capability back on: searched again, and Settings says connected
	// although the table's own link stays off
	f.set(func() { notes.state = "on" })
	a.forgetCapable()
	if sec = find(a.alice); snippet(sec) != "through the contract" {
		t.Fatalf("back on: section %v", sec)
	}
	_, _, list := a.call(a.alice, "GET", "/api/v1/apps", nil)
	for _, x := range list {
		row := x.(map[string]any)
		if row["slug"] != "notes" {
			continue
		}
		// notes:write (keeping a result as a note) is the table's own link and is off
		if row["via"] != "core" || !strings.Contains(row["detail"].(string), "notes:write") || strings.Contains(row["detail"].(string), "notes:read") || row["discovered"] != false || row["icon"] != "" || row["bang"] != "notes" {
			t.Fatalf("notes in Settings = %v", row)
		}
	}
	// the person's own browser address for the app still decides where the links go
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{"notes": map[string]any{"mode": "core", "public": "https://my-notes.example/"}}})
	sec = find(a.alice)
	if it := sec["items"].([]any)[0].(map[string]any); it["url"] != "https://my-notes.example/note/3" || it["snippet"] != "through the contract" || sec["more"] != "https://my-notes.example/" {
		t.Fatalf("own address: section %v", sec)
	}
	// a direct connection stays direct: the person's own address and token
	direct := &capApp{answer: func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":3,"title":"Sourdough notes","snippet":"my own copy"}]`)
	}}
	direct.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		direct.mu.Lock()
		direct.paths, direct.auth = append(direct.paths, r.URL.Path), r.Header.Get("Authorization")
		direct.mu.Unlock()
		direct.answer(w, r)
	}))
	t.Cleanup(direct.srv.Close)
	a.ok(a.bob, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{"notes": map[string]any{"mode": "direct", "url": direct.srv.URL, "token": "pnt_bob", "public": "https://notes.example"}}})
	sec = find(a.bob)
	auth, _ = direct.last()
	if got := direct.asked(); snippet(sec) != "my own copy" || len(got) != 1 || got[0] != "/api/v1/search" || auth != "Bearer pnt_bob" || len(notes.asked()) != 0 {
		t.Fatalf("direct: section %v, asked %v as %q, Core's copy asked %v", sec, got, auth, notes.asked())
	}
	// and switched off in Settings stays off
	a.ok(a.bob, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{"notes": map[string]any{"mode": "off"}}})
	if sec = find(a.bob); sec["off"] != true || len(notes.asked()) != 0 {
		t.Fatalf("off in Settings: section %v, asked %v", sec, notes.asked())
	}
	// the table keeps its bang; the app's own extra one is not taken over
	if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("!notes bread"), nil); m["redirect"] != "/search?cat=apps&app=notes&q=bread" {
		t.Fatalf("!notes = %v", m["redirect"])
	}
	if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("!jot bread"), nil); m["redirect"] != nil && m["redirect"] != "" {
		t.Fatalf("!jot = %v", m["redirect"])
	}
}

// A link that is off says so in plain words, an app that fails says that it
// did not answer, and neither holds up the apps that work. Each person has
// their own links.
func TestDiscoveredAppsOffAndFailing(t *testing.T) {
	rec := recipesApp()
	rec.stateFor = map[string]string{"bob@example.com": "off_person"}
	garden := &capApp{slug: "garden", name: "Pocket Garden", public: "https://garden.example", state: "off_admin", search: &core.Searchable{Kinds: []string{"plant"}},
		answer: func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"items":[{"title":"never asked"}]}`)
		}}
	broken := &capApp{slug: "broken", name: "Pocket Broken", public: "https://broken.example",
		answer: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"the app could not answer"}`)
		}}
	f := newCapCore(t, rec, garden, broken)
	a := capBox(t, f)
	secs := sections(a.ok(a.alice, "GET", "/api/v1/search/apps?q=bread", nil))
	if s := secs["recipes"]; len(s["items"].([]any)) != 4 || s["error"] != nil {
		t.Fatalf("alice's recipes = %v", s)
	}
	if s := secs["garden"]; s["off"] != true || len(s["items"].([]any)) != 0 || s["error"] != "Pocket Garden is not set up: the Search → Pocket Garden link is switched off in Pocket Core (Links)" || len(garden.asked()) != 0 {
		t.Fatalf("garden = %v (asked %v)", s, garden.asked())
	}
	if s := secs["broken"]; s["off"] != nil || len(s["items"].([]any)) != 0 || s["error"] != "Pocket Broken did not answer: the app could not answer (HTTP 500)" {
		t.Fatalf("broken = %v", s)
	}
	// Bob switched his own link to Recipes off: nothing of Alice's, and no call to the app
	rec.reset()
	secs = sections(a.ok(a.bob, "GET", "/api/v1/search/apps?q=bread", nil))
	if s := secs["recipes"]; s["off"] != true || len(s["items"].([]any)) != 0 || !strings.Contains(s["error"].(string), "you switched the Search → Pocket Recipes link off") || len(rec.asked()) != 0 {
		t.Fatalf("bob's recipes = %v (asked %v)", s, rec.asked())
	}
	// switched off while Search still believes it is on: Core's refusal reads the same
	f.set(func() { rec.state = "off_admin" })
	if s := sections(a.ok(a.alice, "GET", "/api/v1/search/apps?q=bread&app=recipes", nil))["recipes"]; s["off"] != true || !strings.Contains(s["error"].(string), "switched off in Pocket Core (Links)") {
		t.Fatalf("just switched off = %v", s)
	}
	// an app that is not there at all: no section, like an unknown app before
	if m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=bread&app=nowhere", nil); len(m["sections"].([]any)) != 0 {
		t.Fatalf("unknown app = %v", m["sections"])
	}
}

// The shortcuts an app brings in its manifest work for the person, except the
// ones that already mean something.
func TestDiscoveredAppBangs(t *testing.T) {
	rec := recipesApp() // recipes, cook; w is Wikipedia's, notes is Pocket Notes', "Bad!" and "x" are not shortcuts
	cellar := &capApp{slug: "cellar", name: "Pocket Cellar", public: "https://cellar.example",
		search: &core.Searchable{Bangs: []string{"cook", "wine", "recipes", "a1", "waytoolongtobeashortcutword", "UPPER", "wi-ne"}},
		answer: func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"items":[]}`) }}
	f := newCapCore(t, rec, cellar)
	a := capBox(t, f)
	for q, want := range map[string]string{
		"!recipes rye bread": "/search?cat=apps&app=recipes&q=rye+bread",
		"rye !recipes":       "/search?cat=apps&app=recipes&q=rye",
		"!recipes":           "/search?cat=apps&app=recipes",
		// Pocket Cellar comes first by name, so the word both want is its;
		// "recipes" is Pocket Recipes' own name and stays with it
		"!cook rye": "/search?cat=apps&app=cellar&q=rye",
		"!wine rye": "/search?cat=apps&app=cellar&q=rye",
		"!a1 rye":   "/search?cat=apps&app=cellar&q=rye",
		// taken words keep their meaning
		"!w rye":     "https://en.wikipedia.org/wiki/Special:Search?search=rye",
		"!notes rye": "/search?cat=apps&app=notes&q=rye",
	} {
		if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape(q), nil); m["redirect"] != want {
			t.Errorf("%q → %v, want %s", q, m["redirect"], want)
		}
	}
	// words that are not shortcuts were never taken
	for _, q := range []string{"!waytoolongtobeashortcutword rye", "!upper rye", "!wi-ne rye", "!bad rye"} {
		if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape(q), nil); m["redirect"] != nil && m["redirect"] != "" {
			t.Errorf("%q → %v, want an ordinary search", q, m["redirect"])
		}
	}
	// "x" is a built-in bang, whatever an app says
	if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("!x rye"), nil); m["redirect"] != "https://x.com/search?q=rye" {
		t.Errorf("!x → %v", m["redirect"])
	}
	// the browser's search address jumps at once
	code, _, h := a.raw(a.bob, "GET", "/search?q="+url.QueryEscape("!wine merlot"), nil, "")
	if code != 302 || h.Get("Location") != "/search?cat=apps&app=cellar&q=merlot" {
		t.Fatalf("/search → %d %q", code, h.Get("Location"))
	}
	// the list of bangs and the suggestions know them, each once
	count := map[string]int{}
	for _, b := range a.ok(a.alice, "GET", "/api/v1/bangs", nil)["builtin"].([]any) {
		count[b.(map[string]any)["trigger"].(string)+" "+b.(map[string]any)["name"].(string)]++
	}
	if count["recipes Pocket Recipes"] != 1 || count["wine Pocket Cellar"] != 1 || count["cook Pocket Cellar"] != 1 || count["cook Pocket Recipes"] != 0 || count["w Pocket Recipes"] != 0 || count["notes Pocket Recipes"] != 0 || count["notes Pocket Notes"] != 1 {
		t.Fatalf("bangs = %v", count)
	}
	sug := a.ok(a.alice, "GET", "/api/v1/suggest?q="+url.QueryEscape("!reci"), nil)["bangs"].([]any)
	if len(sug) != 1 || sug[0].(map[string]any)["name"] != "Pocket Recipes" || sug[0].(map[string]any)["group"] != "Your Pocket apps" {
		t.Fatalf("suggested = %v", sug)
	}
	// a person's own bang still wins
	a.ok(a.alice, "POST", "/api/v1/bangs", map[string]any{"trigger": "wine", "name": "Vivino", "url": "https://www.vivino.com/search/wines?q={q}"})
	if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("!wine merlot"), nil); m["redirect"] != "https://www.vivino.com/search/wines?q=merlot" {
		t.Fatalf("own bang = %v", m["redirect"])
	}
}

// Settings lists the apps found through Core with their own icon; they can be
// switched off or left out of the results, but have no direct connection.
func TestDiscoveredAppInSettings(t *testing.T) {
	rec := recipesApp()
	garden := &capApp{slug: "garden", name: "Pocket Garden", public: "https://garden.example", state: "off_admin",
		answer: func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"items":[]}`) }}
	f := newCapCore(t, rec, garden)
	a := capBox(t, f)
	rows := func(auth string) map[string]map[string]any {
		t.Helper()
		code, _, list := a.call(auth, "GET", "/api/v1/apps", nil)
		if code != 200 || len(list) != len(pocketApps)+2 {
			t.Fatalf("GET /apps → %d, %d rows (the table has %d)", code, len(list), len(pocketApps))
		}
		out := map[string]map[string]any{}
		for _, x := range list {
			out[x.(map[string]any)["slug"].(string)] = x.(map[string]any)
		}
		return out
	}
	all := rows(a.alice)
	r := all["recipes"]
	if r["name"] != "Pocket Recipes" || r["icon"] != "https://recipes.example/favicon.svg" || r["discovered"] != true || r["reason"] != "Your recipes, by name and ingredient" ||
		r["can_search"] != true || r["bang"] != "recipes" || r["mode"] != "core" || r["via"] != "core" || r["detail"] != "" || r["in_results"] != true || fmt.Sprint(r["scopes"]) != "[pocket:search]" || r["send"] != nil {
		t.Fatalf("recipes = %v", r)
	}
	// its link is off: not connected, in plain words; it said nothing about itself
	if g := all["garden"]; g["via"] != "" || g["mode"] != "core" || g["detail"] != "the Search → Pocket Garden link is switched off in Pocket Core (Links)" || g["reason"] != "Search what you keep in Pocket Garden beside the web" || g["bang"] != nil || g["icon"] != "" {
		t.Fatalf("garden = %v", g)
	}
	if n := all["notes"]; n["discovered"] != false || n["icon"] != "" {
		t.Fatalf("notes = %v", n)
	}
	// left out of the results: skipped beside the web, still there when asked for by name
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{"recipes": map[string]any{"search": false}}})
	if r := rows(a.alice)["recipes"]; r["in_results"] != false || r["mode"] != "core" {
		t.Fatalf("left out: %v", r)
	}
	if _, in := sections(a.ok(a.alice, "GET", "/api/v1/search/apps?q=bread", nil))["recipes"]; in {
		t.Fatal("Recipes searched although left out of the results")
	}
	if s := sections(a.ok(a.alice, "GET", "/api/v1/search/apps?q=bread&app=recipes", nil))["recipes"]; len(s["items"].([]any)) != 4 {
		t.Fatalf("asked for by name: %v", s)
	}
	// that was Alice's choice only
	if r := rows(a.bob)["recipes"]; r["in_results"] != true {
		t.Fatalf("bob's recipes = %v", r)
	}
	// no address and token for an app found through Core
	code, m, _ := a.call(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{"recipes": map[string]any{"mode": "direct", "url": "http://evil.example", "token": "x"}}})
	if code != 400 || m["error"] != "Pocket Recipes is reached through Pocket Core only: choose Pocket Core or Off" {
		t.Fatalf("direct → %d %v", code, m)
	}
	// an address sent along with a mode that is allowed is not kept either
	st := a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"recipes": map[string]any{"mode": "off", "url": "http://evil.example", "token": "secret-token", "public": "http://evil.example"},
		"nowhere": map[string]any{"mode": "off"}}})
	apps := st["apps"].(map[string]any)
	if got := apps["recipes"].(map[string]any); got["mode"] != "off" || got["url"] != "" || got["token"] != "" || got["public"] != "" || got["search"] != false {
		t.Fatalf("saved = %v", got)
	}
	if _, kept := apps["nowhere"]; kept {
		t.Fatalf("an app nobody knows was saved: %v", apps)
	}
	// switched off: Search does not talk to it
	rec.reset()
	if r := rows(a.alice)["recipes"]; r["mode"] != "off" || r["via"] != "" || r["detail"] != "switched off" {
		t.Fatalf("off: %v", r)
	}
	if s := sections(a.ok(a.alice, "GET", "/api/v1/search/apps?q=bread&app=recipes", nil))["recipes"]; s["off"] != true || s["error"] != "Pocket Recipes is switched off in Settings → Connected apps" || len(rec.asked()) != 0 {
		t.Fatalf("off: %v (asked %v)", s, rec.asked())
	}
	// Core hears about the one thing it cannot see: the app switched off here
	modes, err := a.s.connModes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, m := range modes {
		if m.Target == "recipes" || m.Target == "garden" || m.Target == "nowhere" {
			if m.User != "alice@example.com" || m.Target != "recipes" || m.Mode != "off" {
				t.Fatalf("mode reported: %+v", m)
			}
			found++
		}
	}
	if found != 1 || len(modes) != 2*len(pocketApps)+1 {
		t.Fatalf("%d modes, %d about Recipes", len(modes), found)
	}
	// and back on
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{"recipes": map[string]any{"mode": "core", "search": true}}})
	if s := sections(a.ok(a.alice, "GET", "/api/v1/search/apps?q=bread", nil))["recipes"]; len(s["items"].([]any)) != 4 {
		t.Fatalf("back on: %v", s)
	}
}

// Without Pocket Core, with an older Core, with a Core that lists nothing and
// with one that does not answer, Search is exactly what it was: the table's
// apps and nothing else.
func TestNoCapabilitiesNothingChanges(t *testing.T) {
	legacy := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `[{"id":3,"title":"Sourdough notes","snippet":"the table's way"}]`)
	}
	searchable := 0
	for _, p := range pocketApps {
		if p.search != nil {
			searchable++
		}
	}
	same := func(t *testing.T, a *app) {
		t.Helper()
		code, _, list := a.call(a.alice, "GET", "/api/v1/apps", nil)
		if code != 200 || len(list) != len(pocketApps) {
			t.Fatalf("GET /apps → %d, %d rows, want the table's %d", code, len(list), len(pocketApps))
		}
		for i, x := range list {
			if row := x.(map[string]any); row["slug"] != pocketApps[i].slug || row["discovered"] != false || row["icon"] != "" {
				t.Fatalf("row %d = %v", i, row)
			}
		}
		if secs := a.ok(a.alice, "GET", "/api/v1/search/apps?q=sourdough", nil)["sections"].([]any); len(secs) != searchable {
			t.Fatalf("%d sections, want the table's %d", len(secs), searchable)
		}
		if n := len(a.ok(a.alice, "GET", "/api/v1/bangs", nil)["builtin"].([]any)); n != len(builtinBangs)+len(appBangs()) {
			t.Fatalf("%d bangs, want %d", n, len(builtinBangs)+len(appBangs()))
		}
		if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("!recipes rye"), nil); m["redirect"] != nil && m["redirect"] != "" {
			t.Fatalf("!recipes = %v", m["redirect"])
		}
		if code, _, _ := a.call(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{"recipes": map[string]any{"mode": "off"}}}); code != 200 {
			t.Fatalf("settings → %d", code)
		}
		if st := a.ok(a.alice, "GET", "/api/v1/settings", nil); len(st["apps"].(map[string]any)) != 0 {
			t.Fatalf("an unknown app was saved: %v", st["apps"])
		}
	}
	t.Run("no Core", func(t *testing.T) {
		a := newApp(t)
		same(t, a)
		a.s.capMu.Lock()
		defer a.s.capMu.Unlock()
		if len(a.s.capable) != 0 {
			t.Fatalf("something was remembered without a Core: %v", a.s.capable)
		}
	})
	t.Run("an older Core", func(t *testing.T) {
		// it has the table's links, ignores ?scope= and never names the apps
		notes := &capApp{slug: "notes", name: "Pocket Notes", public: "https://notes.example", legacy: map[string]bool{"notes:read": true, "notes:write": true}, answer: legacy}
		f := newCapCore(t, notes, recipesApp())
		f.old = true
		a := capBox(t, f)
		same(t, a)
		sec := sections(a.ok(a.alice, "GET", "/api/v1/search/apps?q=sourdough&app=notes", nil))["notes"]
		if got := notes.asked(); len(sec["items"].([]any)) != 1 || len(got) == 0 || got[len(got)-1] != "/api/v1/search" {
			t.Fatalf("notes = %v, asked %v", sec, got)
		}
		for _, p := range notes.asked() {
			if p == core.SearchPath {
				t.Fatalf("the contract's route was asked on an older Core: %v", notes.asked())
			}
		}
	})
	t.Run("nothing offers it", func(t *testing.T) {
		a := capBox(t, newCapCore(t))
		same(t, a)
	})
	t.Run("Core does not answer", func(t *testing.T) {
		notes := &capApp{slug: "notes", name: "Pocket Notes", public: "https://notes.example", search: &core.Searchable{}, legacy: map[string]bool{"notes:read": true, "notes:write": true}, answer: legacy}
		f := newCapCore(t, notes, recipesApp())
		f.down = true
		a := capBox(t, f)
		same(t, a)
		// the table's adapter still works, and Core is not asked again on every search
		sec := sections(a.ok(a.alice, "GET", "/api/v1/search/apps?q=sourdough&app=notes", nil))["notes"]
		if n := f.count(); len(sec["items"].([]any)) != 1 || n != 1 {
			t.Fatalf("notes = %v, Core asked %d times", sec, n)
		}
	})
}

// Many searches at once, for two people: the memory of who can be searched
// holds (run with -race), and nobody gets the other's list.
func TestCapableAppsPerPersonAtOnce(t *testing.T) {
	rec := recipesApp()
	rec.stateFor = map[string]string{"bob@example.com": "off_person"}
	a := capBox(t, newCapCore(t, rec))
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			auth, on := a.alice, true
			if i%2 == 1 {
				auth, on = a.bob, false
			}
			code, m, _ := a.call(auth, "GET", "/api/v1/search/apps?q=bread&app=recipes", nil)
			if code != 200 {
				t.Errorf("search %d → %d", i, code)
				return
			}
			if s := sections(m)["recipes"]; (len(s["items"].([]any)) == 4) != on || (s["off"] == true) == on {
				t.Errorf("search %d (on=%v) = %v", i, on, s)
			}
			if i%3 == 0 {
				a.forgetCapable()
			}
		}(i)
	}
	wg.Wait()
}
