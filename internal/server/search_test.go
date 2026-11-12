package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-search/internal/config"
	"github.com/abjelosevic88/pocket-homelab-search/internal/engine"
	"github.com/abjelosevic88/pocket-homelab-search/internal/read"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string // admin, owner of the box
	bob   string
	ids   map[string]int64
	eng   *fakeEngine
}

// fakeEngine answers like SearXNG's JSON format and remembers what it was asked.
type fakeEngine struct {
	mu    sync.Mutex
	asked []url.Values
	srv   *httptest.Server
	pages map[string]string        // other paths: a tiny web site for the reading view
	delay time.Duration            // how long /search takes
	slow  map[string]time.Duration // longer for a search asking only these engines
}

func (f *fakeEngine) last() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.asked) == 0 {
		return nil
	}
	return f.asked[len(f.asked)-1]
}

func newApp(t *testing.T) *app {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.FetchPrivate = true // the fake site runs on localhost
	cfg.BaseURL = "http://search.test"
	cfg.EngineTimeout = 5e9
	f := &fakeEngine{pages: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			f.mu.Lock()
			f.asked = append(f.asked, r.URL.Query())
			d := f.delay + f.slow[r.URL.Query().Get("engines")]
			f.mu.Unlock()
			time.Sleep(d)
			if r.URL.Query().Get("format") != "json" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("engines") == "wikidata" {
				_, _ = io.WriteString(w, `{"query":"x","results":[],"answers":[],"corrections":[],"suggestions":[],"unresponsive_engines":[],
				"infoboxes":[{"infobox":"X","id":"https://en.wikipedia.org/wiki/X","content":"","urls":[{"title":"Official site","url":"https://x.example","official":true}],"attributes":[{"label":"Born","value":"1900"},{"label":"Died","value":"1980"}]}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"query":"x","results":[
				{"url":"https://www.pinterest.com/pin/1","title":"Pin","content":"a pin","engine":"google","engines":["google"],"score":3,"category":"general"},
				{"url":"https://medium.com/post","title":"Post","content":"words","engine":"brave","engines":["brave","google"],"score":2.5,"category":"general"},
				{"url":"https://example.org/a","title":"Example A","content":"aaa","engine":"brave","engines":["brave"],"score":2,"category":"general","publishedDate":"2026-10-01T00:00:00"},
				{"url":"https://en.wikipedia.org/wiki/X","title":"X - Wikipedia","content":"wiki","engine":"wikipedia","engines":["wikipedia"],"score":1,"category":"general"}
			],"answers":["42"],"corrections":[],"infoboxes":[{"infobox":"X","id":"https://en.wikipedia.org/wiki/X","content":"About X","urls":[{"title":"Wikipedia","url":"https://en.wikipedia.org/wiki/X"}],"attributes":[{"label":"Born","value":"1900"}]}],
			"suggestions":["x y"],"unresponsive_engines":[["duckduckgo","CAPTCHA"]]}`)
		case "/autocompleter":
			_ = json.NewEncoder(w).Encode([]any{r.URL.Query().Get("q"), []string{"golang", "goland"}})
		case "/config":
			_, _ = io.WriteString(w, `{"engines":[{"name":"google","categories":["general"],"enabled":true,"shortcut":"go"},{"name":"brave","categories":["general"],"enabled":true,"shortcut":"br"},{"name":"mojeek","categories":["general"],"enabled":false,"shortcut":"mjk"},{"name":"wikidata","categories":["general"],"enabled":true,"shortcut":"wd"}]}`)
		default:
			f.mu.Lock()
			body, ok := f.pages[r.URL.Path]
			f.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			if strings.HasSuffix(r.URL.Path, ".png") {
				w.Header().Set("Content-Type", "image/png")
			} else {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
			}
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(f.srv.Close)
	cfg.SearXNGURL = f.srv.URL
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, ids: map[string]int64{}, eng: f}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) raw(auth, method, path string, body io.Reader, ct string) (int, []byte, http.Header) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cl.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b, res.Header
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	code, raw, _ := a.raw(auth, method, path, rd, "application/json")
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return code, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func urls(m map[string]any) []string {
	var out []string
	for _, r := range m["results"].([]any) {
		out = append(out, r.(map[string]any)["url"].(string))
	}
	return out
}

func TestSearchAppliesOnlyMyRules(t *testing.T) {
	a := newApp(t)
	a.ok(a.alice, "POST", "/api/v1/rules", map[string]any{"domain": "https://www.Pinterest.com/x", "action": "block"})
	a.ok(a.alice, "POST", "/api/v1/rules", map[string]any{"domain": "wikipedia.org", "action": "pin"})
	a.ok(a.alice, "POST", "/api/v1/rules", map[string]any{"domain": "medium.com", "action": "lower"})

	m := a.ok(a.alice, "GET", "/api/v1/search?q=x", nil)
	got := urls(m)
	want := []string{"https://en.wikipedia.org/wiki/X", "https://example.org/a", "https://medium.com/post"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("alice's order = %v, want %v", got, want)
	}
	if m["hidden"].(float64) != 1 || m["hidden_sites"].([]any)[0] != "pinterest.com" {
		t.Fatalf("hidden = %v %v", m["hidden"], m["hidden_sites"])
	}
	first := m["results"].([]any)[0].(map[string]any)
	if first["rule"] != "pin" || first["domain"] != "en.wikipedia.org" {
		t.Fatalf("first = %v", first)
	}
	if c := m["results"].([]any)[2].(map[string]any)["content"]; c != "words" {
		t.Fatalf("content = %q", c)
	}
	if m["answers"].([]any)[0].(map[string]any)["text"] != "42" || len(m["infoboxes"].([]any)) != 1 || len(m["unresponsive"].([]any)) != 1 {
		t.Fatalf("extras = %v %v %v", m["answers"], m["infoboxes"], m["unresponsive"])
	}

	// Bob has no rules: the engine's order, nothing hidden
	m = a.ok(a.bob, "GET", "/api/v1/search?q=x&nohistory=1", nil)
	if got := urls(m); len(got) != 4 || got[0] != "https://www.pinterest.com/pin/1" {
		t.Fatalf("bob's results = %v", got)
	}
	// the rule counted what it changed
	rules := a.st
	list, _ := rules.ListRules(context.Background(), a.ids["Alice"])
	for _, r := range list {
		if r.Hits == 0 {
			t.Fatalf("rule %s has no hits", r.Domain)
		}
	}
}

func TestSafeSearchAndEnginesFollowMySettings(t *testing.T) {
	a := newApp(t)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"safe_search": 2, "language": "de", "engines_off": []string{"google"}, "engines_on": []string{"mojeek"}})
	a.ok(a.alice, "GET", "/api/v1/search?q=x&cat=general&time=week&page=2", nil)
	q := a.eng.last()
	if q.Get("safesearch") != "2" || q.Get("language") != "de" || q.Get("time_range") != "week" || q.Get("pageno") != "2" || q.Get("categories") != "" {
		t.Fatalf("engine asked %v", q)
	}
	if q.Get("engines") != "brave,mojeek" {
		t.Fatalf("engines = %q, want brave,mojeek", q.Get("engines"))
	}
	// Bob's settings are his own
	a.ok(a.bob, "GET", "/api/v1/search?q=y&full=1", nil)
	if q := a.eng.last(); q.Get("safesearch") != "1" || strings.Contains(q.Get("engines"), "mojeek") || q.Get("language") != "auto" {
		t.Fatalf("bob asked %v", q)
	}
	if m := a.ok(a.bob, "GET", "/api/v1/settings", nil); m["safe_search"].(float64) != 1 {
		t.Fatalf("bob's safe search = %v", m["safe_search"])
	}
	// every engine of a category off → a plain message, no search
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"engines_off": []string{"google", "brave"}, "engines_on": []string{}})
	if code, m, _ := a.call(a.alice, "GET", "/api/v1/search?q=z", nil); code != 502 || !strings.Contains(m["error"].(string), "switched off") {
		t.Fatalf("all off → %d %v", code, m)
	}
}

func TestOtherPeoplesThingsAreNotFound(t *testing.T) {
	a := newApp(t)
	rule := a.ok(a.alice, "POST", "/api/v1/rules", map[string]any{"domain": "example.com", "action": "boost"})
	bang := a.ok(a.alice, "POST", "/api/v1/bangs", map[string]any{"trigger": "!ex", "url": "https://example.com/?s={q}"})
	a.ok(a.alice, "GET", "/api/v1/search?q=secret+stuff", nil)
	hist, _, _ := a.st.ListHistory(context.Background(), a.ids["Alice"], store.HistoryFilter{})
	if len(hist) != 1 || hist[0].Query != "secret stuff" {
		t.Fatalf("history = %+v", hist)
	}
	rid, bid := int64(rule["id"].(float64)), int64(bang["id"].(float64))
	for _, c := range []struct{ method, path string }{
		{"PUT", fmt.Sprintf("/api/v1/rules/%d", rid)},
		{"DELETE", fmt.Sprintf("/api/v1/rules/%d", rid)},
		{"PUT", fmt.Sprintf("/api/v1/bangs/%d", bid)},
		{"DELETE", fmt.Sprintf("/api/v1/bangs/%d", bid)},
		{"DELETE", fmt.Sprintf("/api/v1/history/%d", hist[0].ID)},
	} {
		if code, _, _ := a.call(a.bob, c.method, c.path, map[string]any{"domain": "x.com", "action": "block", "trigger": "zz", "url": "https://x.com/{q}"}); code != 404 {
			t.Fatalf("bob %s %s → %d, want 404", c.method, c.path, code)
		}
	}
	// Bob's lists are empty; his own "!ex" is free
	if _, _, l := a.call(a.bob, "GET", "/api/v1/rules", nil); len(l) != 0 {
		t.Fatalf("bob sees rules %v", l)
	}
	if m := a.ok(a.bob, "GET", "/api/v1/history", nil); m["total"].(float64) != 0 {
		t.Fatalf("bob sees history %v", m)
	}
	if m := a.ok(a.bob, "GET", "/api/v1/bangs", nil); len(m["mine"].([]any)) != 0 {
		t.Fatalf("bob sees bangs %v", m["mine"])
	}
	if code, _, _ := a.call(a.bob, "POST", "/api/v1/bangs", map[string]any{"trigger": "ex", "url": "https://bob.example/?q={q}"}); code != 201 {
		t.Fatalf("bob's own !ex → %d", code)
	}
	// Bob clearing everything leaves Alice's history alone
	a.ok(a.bob, "POST", "/api/v1/history/clear", map[string]any{"range": "all"})
	if hist, _, _ := a.st.ListHistory(context.Background(), a.ids["Alice"], store.HistoryFilter{}); len(hist) != 1 {
		t.Fatalf("alice's history after bob cleared his = %d", len(hist))
	}
}

func TestBangs(t *testing.T) {
	for in, want := range map[string][2]string{"!w eiffel tower": {"w", "eiffel tower"}, "eiffel tower !W": {"w", "eiffel tower"}, "!yt": {"yt", ""}, "a!b c": {"", ""}, "price !": {"", ""}} {
		tr, rest, _ := splitBang(in)
		if tr != want[0] || rest != want[1] {
			t.Errorf("splitBang(%q) = %q %q, want %q %q", in, tr, rest, want[0], want[1])
		}
	}
	a := newApp(t)
	m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("!w eiffel tower"), nil)
	if m["redirect"] != "https://en.wikipedia.org/wiki/Special:Search?search=eiffel%20tower" {
		t.Fatalf("redirect = %v", m["redirect"])
	}
	if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("cats !i"), nil); m["redirect"] != "/search?cat=images&q=cats" {
		t.Fatalf("in-app bang = %v", m["redirect"])
	}
	// my own !w wins over Wikipedia; a bang leaves no history
	a.ok(a.alice, "POST", "/api/v1/bangs", map[string]any{"trigger": "w", "name": "Wiktionary", "url": "https://en.wiktionary.org/w/index.php?search={q}"})
	if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("!w run"), nil); m["redirect"] != "https://en.wiktionary.org/w/index.php?search=run" {
		t.Fatalf("own bang = %v", m["redirect"])
	}
	if hist, _, _ := a.st.ListHistory(context.Background(), a.ids["Alice"], store.HistoryFilter{}); len(hist) != 0 {
		t.Fatalf("bangs left history %+v", hist)
	}
	// the browser's search address jumps at once
	code, _, h := a.raw(a.bob, "GET", "/search?q="+url.QueryEscape("!gh pocket"), nil, "")
	if code != 302 || h.Get("Location") != "https://github.com/search?q=pocket" {
		t.Fatalf("/search → %d %q", code, h.Get("Location"))
	}
	// bad bangs are refused in plain words
	for _, b := range []map[string]any{{"trigger": "has space", "url": "https://x.com/{q}"}, {"trigger": "ok", "url": "https://x.com/"}, {"trigger": "ok", "url": "javascript:alert({q})"}} {
		if code, _, _ := a.call(a.alice, "POST", "/api/v1/bangs", b); code != 400 {
			t.Fatalf("bang %v → %d", b, code)
		}
	}
}

func TestHistoryOffKeepsNothing(t *testing.T) {
	a := newApp(t)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"history": false})
	a.ok(a.alice, "GET", "/api/v1/search?q=private", nil)
	if code, _, _ := a.call(a.alice, "POST", "/api/v1/history/open", map[string]any{"url": "https://example.org/a", "title": "A"}); code != 204 {
		t.Fatalf("open → %d", code)
	}
	if m := a.ok(a.alice, "GET", "/api/v1/history", nil); m["total"].(float64) != 0 {
		t.Fatalf("history while paused: %v", m)
	}
	if m := a.ok(a.alice, "GET", "/api/v1/suggest?q=pri", nil); len(m["history"].([]any)) != 0 || len(m["suggestions"].([]any)) != 2 {
		t.Fatalf("suggest = %v", m)
	}
}

func TestExportImportIsIdempotent(t *testing.T) {
	a := newApp(t)
	a.ok(a.alice, "POST", "/api/v1/rules", map[string]any{"domain": "example.com", "action": "boost", "note": "good"})
	a.ok(a.alice, "POST", "/api/v1/rules", map[string]any{"domain": "spam.example", "action": "block"})
	a.ok(a.alice, "POST", "/api/v1/bangs", map[string]any{"trigger": "ex", "url": "https://example.com/?s={q}"})
	a.ok(a.alice, "GET", "/api/v1/search?q=one", nil)
	_, doc, _ := a.raw(a.alice, "GET", "/api/v1/export.json", nil, "")
	for i := 0; i < 2; i++ {
		code, b, _ := a.raw(a.bob, "POST", "/api/v1/import?history=1", bytes.NewReader(doc), "application/json")
		if code != 200 {
			t.Fatalf("import %d → %d %s", i, code, b)
		}
	}
	ctx := context.Background()
	rules, _ := a.st.ListRules(ctx, a.ids["Bob"])
	bangs, _ := a.st.ListBangs(ctx, a.ids["Bob"])
	hist, _, _ := a.st.ListHistory(ctx, a.ids["Bob"], store.HistoryFilter{})
	if len(rules) != 2 || len(bangs) != 1 || len(hist) != 1 {
		t.Fatalf("after two imports bob has %d rules, %d bangs, %d history", len(rules), len(bangs), len(hist))
	}
	// a plain list of sites
	code, b, _ := a.raw(a.bob, "POST", "/api/v1/import", strings.NewReader("# my list\npinterest.com\nmdn.io raise\nnot a domain\n"), "text/plain")
	var out map[string]float64
	_ = json.Unmarshal(b, &out)
	if code != 200 || out["rules"] != 2 || out["skipped"] != 1 {
		t.Fatalf("list import → %d %s", code, b)
	}
}

func TestReadingViewProxiesPictures(t *testing.T) {
	a := newApp(t)
	para := strings.Repeat("This is a long enough paragraph about the subject to count as an article. ", 6)
	a.eng.pages["/article"] = `<html><head><title>A story</title><meta property="og:site_name" content="Example News"></head><body>
		<nav>menu</nav><article><h1>A story</h1><p>` + para + `</p><img src="/pic.png" alt="pic"><script>alert(1)</script>
		<iframe src="https://www.youtube.com/embed/abc"></iframe><p>` + para + `</p></article></body></html>`
	m := a.ok(a.alice, "GET", "/api/v1/read?url="+url.QueryEscape(a.eng.srv.URL+"/article"), nil)
	content := m["content"].(string)
	if !strings.Contains(content, `src="/api/v1/img?u=`+url.QueryEscape(a.eng.srv.URL+"/pic.png")) {
		t.Fatalf("picture not proxied: %s", content)
	}
	if strings.Contains(content, "<script") || strings.Contains(content, "iframe") || m["embeds"].(float64) != 1 {
		t.Fatalf("unsafe content or embed count: %v %s", m["embeds"], content)
	}
	a.eng.pages["/pic.png"] = "\x89PNG\r\n\x1a\n0000"
	code, _, h := a.raw(a.alice, "GET", "/api/v1/img?u="+url.QueryEscape(a.eng.srv.URL+"/pic.png"), nil, "")
	if code != 200 || h.Get("Content-Type") != "image/png" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("img → %d %v", code, h)
	}
	// the picture proxy does not fetch pages
	if code, _, _ := a.raw(a.alice, "GET", "/api/v1/img?u="+url.QueryEscape(a.eng.srv.URL+"/article"), nil, ""); code != 502 {
		t.Fatalf("img of a page → %d", code)
	}
	if code, _, _ := a.call(a.alice, "GET", "/api/v1/read?url=file:///etc/passwd", nil); code != 400 {
		t.Fatalf("file url → %d", code)
	}
}

func TestPrivateAddressesRefusedByDefault(t *testing.T) {
	a := newApp(t)
	a.s.fetch = nil
	cfg := *a.s.cfg
	cfg.FetchPrivate = false
	a.s.cfg = &cfg
	a.s.fetch = newFetcherForTest(false)
	if code, m, _ := a.call(a.alice, "GET", "/api/v1/read?url="+url.QueryEscape(a.eng.srv.URL+"/x"), nil); code != 502 || !strings.Contains(m["error"].(string), "private network") {
		t.Fatalf("private read → %d %v", code, m)
	}
}

func TestLinkScopes(t *testing.T) {
	req := func(method, path string) *http.Request { r, _ := http.NewRequest(method, path, nil); return r }
	read := []string{"search:read"}
	for _, c := range []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/v1/search", true}, {"GET", "/api/v1/read", true}, {"GET", "/api/v1/suggest", true}, {"GET", "/api/v1/auth/me", true},
		{"GET", "/api/v1/history", false}, {"POST", "/api/v1/rules", false}, {"GET", "/api/v1/settings", false},
		{"POST", "/api/v1/send/reader", false}, {"GET", "/api/v1/export.json", false}, {"POST", "/api/v1/search", false},
	} {
		if got := linkAllows(read, req(c.method, c.path)); got != c.want {
			t.Errorf("search:read %s %s = %v, want %v", c.method, c.path, got, c.want)
		}
	}
	if linkAllows(nil, req("GET", "/api/v1/search")) {
		t.Error("no scopes may search")
	}
}

func TestEnginePickAndAppsTable(t *testing.T) {
	all := []engine.Info{{Name: "a", Categories: []string{"general"}, Enabled: true}, {Name: "b", Categories: []string{"general", "images"}, Enabled: true}, {Name: "c", Categories: []string{"general"}}}
	if got := engine.Pick(all, "general", nil, nil); got != nil {
		t.Fatalf("no changes → %v", got)
	}
	if got := engine.Pick(all, "general", []string{"a"}, []string{"c"}); strings.Join(got, ",") != "b,c" {
		t.Fatalf("pick = %v", got)
	}
	if got := engine.Pick(all, "images", []string{"a"}, nil); got != nil {
		t.Fatalf("a change elsewhere → %v", got)
	}
	seen := map[string]bool{}
	bangs := map[string]bool{}
	for _, b := range builtinBangs {
		bangs[b.Trigger] = true
	}
	for _, a := range pocketApps {
		if seen[a.slug] {
			t.Fatalf("app %s twice", a.slug)
		}
		seen[a.slug] = true
		if a.search == nil && a.send == nil {
			t.Fatalf("app %s does nothing", a.slug)
		}
		if a.search != nil && (bangs[a.bang] || a.bang == "") {
			t.Fatalf("app %s bang %q missing or clashes with a built-in one", a.slug, a.bang)
		}
		bangs[a.bang] = true
		for _, al := range a.aliases {
			if bangs[al] || !reBangWord.MatchString("!"+al) {
				t.Fatalf("app %s alias %q clashes or is not a bang", a.slug, al)
			}
			bangs[al] = true
		}
		for _, sc := range a.scopes() {
			if sc == "" {
				t.Fatalf("app %s has an empty scope", a.slug)
			}
		}
	}
}

var _ = config.Config{}

func newFetcherForTest(private bool) *read.Fetcher { return read.NewFetcher("test", private) }

// A direct connection to a fake Pocket Notes / Reader: searched beside the
// web with the person's own token, and a result sent to Reader.
func TestYourAppsDirect(t *testing.T) {
	a := newApp(t)
	var gotAuth, gotBody string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/v1/search":
			_, _ = io.WriteString(w, `[{"id":3,"title":"Sourdough notes","snippet":"**feed** it twice","updated_at":"2026-10-30T10:00:00Z"},{"id":4,"title":"","snippet":"untitled"}]`)
		case "/api/v1/saved":
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"article":{"id":7},"created":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"notes":  map[string]any{"mode": "direct", "url": sib.URL, "token": "pnt_secret", "public": "https://notes.example"},
		"reader": map[string]any{"mode": "direct", "url": sib.URL, "token": "prd_secret"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=sourdough&app=notes", nil)
	secs := m["sections"].([]any)
	items := secs[0].(map[string]any)["items"].([]any)
	if len(secs) != 1 || len(items) != 1 || gotAuth != "Bearer pnt_secret" {
		t.Fatalf("sections = %v (auth %q)", secs, gotAuth)
	}
	it := items[0].(map[string]any)
	if it["url"] != "https://notes.example/note/3" || it["snippet"] != "feed it twice" || it["kind"] != "note" {
		t.Fatalf("item = %v", it)
	}
	// Bob has no connection: a quiet "not set up", never Alice's notes
	m = a.ok(a.bob, "GET", "/api/v1/search/apps?q=sourdough&app=notes", nil)
	if s := m["sections"].([]any)[0].(map[string]any); s["off"] != true || len(s["items"].([]any)) != 0 {
		t.Fatalf("bob's notes = %v", s)
	}
	// the token never comes back
	if st := a.ok(a.alice, "GET", "/api/v1/settings", nil); strings.Contains(fmt.Sprint(st["apps"]), "pnt_secret") {
		t.Fatalf("token leaked: %v", st["apps"])
	}
	res := a.ok(a.alice, "POST", "/api/v1/send/reader", map[string]any{"url": "https://example.org/a", "title": "Example A", "query": "x"})
	if res["item_url"] != sib.URL+"/a/7" || !strings.Contains(gotBody, `"url":"https://example.org/a"`) {
		t.Fatalf("send = %v body %s", res, gotBody)
	}
	// the result page shows where it went
	for _, r := range a.ok(a.alice, "GET", "/api/v1/search?q=x", nil)["results"].([]any) {
		h := r.(map[string]any)
		if h["url"] == "https://example.org/a" && len(h["saved"].([]any)) != 1 {
			t.Fatalf("saved badge missing: %v", h)
		}
	}
	if code, _, _ := a.call(a.bob, "POST", "/api/v1/send/reader", map[string]any{"url": "https://example.org/a"}); code != 409 {
		t.Fatalf("bob send without connection → %d", code)
	}
	if got := itemURL("https://b.example/", "/b/{board_id}/c/{id}", map[string]any{"id": 5.0, "board_id": 2.0}, []string{"id"}); got != "https://b.example/b/2/c/5" {
		t.Fatalf("route = %s", got)
	}
}

func TestAddressBarSuggestionsWithoutLogin(t *testing.T) {
	a := newApp(t)
	a.ok(a.alice, "GET", "/api/v1/search?q=golang+secret", nil)
	// the browser's address bar, no cookie: the engine's suggestions, nothing personal
	code, b, h := a.raw("", "GET", "/api/v1/suggest?format=opensearch&q=gol", nil, "")
	if code != 200 || h.Get("Content-Type") != "application/x-suggestions+json" || string(b) != "[\"gol\",[\"golang\",\"goland\"]]\n" {
		t.Fatalf("anonymous opensearch → %d %q %s", code, h.Get("Content-Type"), b)
	}
	if code, _, _ := a.raw("", "GET", "/api/v1/suggest?q=gol", nil, ""); code != 401 {
		t.Fatalf("anonymous app suggest → %d", code)
	}
	// signed in: the person's own earlier search comes first
	code, b, _ = a.raw(a.alice, "GET", "/api/v1/suggest?format=opensearch&q=gol", nil, "")
	if code != 200 || !strings.Contains(string(b), "golang secret") {
		t.Fatalf("signed-in opensearch → %d %s", code, b)
	}
}

// A tab hovered (its search starts) and then clicked: one search reaches the engine.
func TestSameSearchAtOnceAsksTheEngineOnce(t *testing.T) {
	a := newApp(t)
	a.eng.delay = 300 * time.Millisecond
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code, _, _ := a.call(a.alice, "GET", "/api/v1/search?q=volcano&cat=videos&nohistory=1", nil); code != 200 {
				t.Errorf("search → %d", code)
			}
		}()
	}
	wg.Wait()
	a.eng.mu.Lock()
	n := len(a.eng.asked)
	a.eng.mu.Unlock()
	if n != 1 {
		t.Fatalf("engine asked %d times, want 1", n)
	}
}

func TestSlowEngineDoesNotHoldTheResults(t *testing.T) {
	a := newApp(t)
	a.eng.slow = map[string]time.Duration{"brave": 800 * time.Millisecond}
	start := time.Now()
	first := a.ok(a.alice, "GET", "/api/v1/search?q=lanes&nohistory=1", nil)
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("first results took %v: the slow engine held them", took)
	}
	if first["pending"] != true || len(first["results"].([]any)) == 0 {
		t.Fatalf("first answer = %v", first)
	}
	// each engine asked on its own
	a.eng.mu.Lock()
	asked := map[string]bool{}
	for _, q := range a.eng.asked {
		asked[q.Get("engines")] = true
	}
	a.eng.mu.Unlock()
	if !asked["google"] || !asked["brave"] {
		t.Fatalf("engines asked %v", asked)
	}
	// naming a category too would make SearXNG ask all of its engines in every lane
	if q := a.eng.last(); q.Get("categories") != "" {
		t.Fatalf("a lane named its category: %v", q)
	}
	full := a.ok(a.alice, "GET", "/api/v1/search?q=lanes&full=1", nil)
	if full["pending"] != nil {
		t.Fatalf("full answer still pending")
	}
	fr, ar := first["results"].([]any), full["results"].([]any)
	for i := range fr {
		if fr[i].(map[string]any)["url"] != ar[i].(map[string]any)["url"] {
			t.Fatalf("result %d moved: %v → %v", i, fr[i], ar[i])
		}
	}
	// both engines found the same pages: no repeats, and both are credited
	if len(ar) != len(fr) {
		t.Fatalf("full has %d results, first %d", len(ar), len(fr))
	}
	// now kept: asked again it is at once and complete
	start = time.Now()
	again := a.ok(a.alice, "GET", "/api/v1/search?q=lanes&nohistory=1", nil)
	if time.Since(start) > 100*time.Millisecond || again["pending"] != nil {
		t.Fatalf("second look not from memory: %v", again["pending"])
	}
}

func TestWikidataJoinsTheCardLater(t *testing.T) {
	a := newApp(t)
	a.eng.slow = map[string]time.Duration{"wikidata": 800 * time.Millisecond}
	start := time.Now()
	first := a.ok(a.alice, "GET", "/api/v1/search?q=card&nohistory=1", nil)
	full := a.ok(a.alice, "GET", "/api/v1/search?q=card&full=1", nil)
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("results waited %v for Wikidata", took)
	}
	if first["card_pending"] != true || full["card_pending"] != true {
		t.Fatalf("card_pending first=%v full=%v", first["card_pending"], full["card_pending"])
	}
	card := a.ok(a.alice, "GET", "/api/v1/search?q=card&card=1", nil)
	boxes := card["infoboxes"].([]any)
	if card["card_pending"] != nil || len(boxes) != 1 {
		t.Fatalf("card = %v", card)
	}
	b := boxes[0].(map[string]any)
	if attrs := b["attributes"].([]any); len(attrs) != 2 || attrs[1].(map[string]any)["label"] != "Died" {
		t.Fatalf("attributes = %v", attrs)
	}
	if links := b["links"].([]any); len(links) != 2 || b["content"] != "About X" {
		t.Fatalf("links = %v, content %v", links, b["content"])
	}
	if n := len(card["results"].([]any)); n != len(full["results"].([]any)) {
		t.Fatalf("card moved results: %d vs %d", n, len(full["results"].([]any)))
	}
	// kept with the card from now on
	again := a.ok(a.alice, "GET", "/api/v1/search?q=card&nohistory=1", nil)
	if again["card_pending"] != nil || len(again["infoboxes"].([]any)[0].(map[string]any)["attributes"].([]any)) != 2 {
		t.Fatalf("second look = %v", again["infoboxes"])
	}
}

func TestLensesArePersonalAndFilter(t *testing.T) {
	a := newApp(t)
	_, _, list := a.call(a.alice, "GET", "/api/v1/lenses", nil)
	if len(list) != len(defaultLenses) {
		t.Fatalf("alice starts with %d lenses, want %d", len(list), len(defaultLenses))
	}
	l := a.ok(a.alice, "POST", "/api/v1/lenses", map[string]any{"name": "Wiki", "icon": "📚", "sites": "https://en.wikipedia.org/x\nexample.org", "words": "history", "hide": "pinterest.com"})
	id := int64(l["id"].(float64))
	if l["sites"] != "en.wikipedia.org\nexample.org" {
		t.Fatalf("sites = %q", l["sites"])
	}
	m := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/search?q=x&lens=%d", id), nil)
	if got := urls(m); strings.Join(got, " ") != "https://example.org/a https://en.wikipedia.org/wiki/X" {
		t.Fatalf("lens results = %v", got)
	}
	if q := a.eng.last().Get("q"); q != "x history (site:en.wikipedia.org OR site:example.org)" {
		t.Fatalf("engine asked %q", q)
	}
	// another person's lens is not theirs to use or change
	for _, c := range []struct{ method, path string }{{"GET", fmt.Sprintf("/api/v1/search?q=x&lens=%d", id)}, {"PUT", fmt.Sprintf("/api/v1/lenses/%d", id)}, {"DELETE", fmt.Sprintf("/api/v1/lenses/%d", id)}} {
		if code, _, _ := a.call(a.bob, c.method, c.path, map[string]any{"name": "mine"}); code != 404 {
			t.Fatalf("bob %s %s → %d", c.method, c.path, code)
		}
	}
	if code, _, _ := a.call(a.alice, "POST", "/api/v1/lenses", map[string]any{"name": "empty"}); code != 400 {
		t.Fatalf("empty lens → %d", code)
	}
	// deleted defaults stay deleted
	for _, x := range list {
		a.call(a.alice, "DELETE", fmt.Sprintf("/api/v1/lenses/%d", int64(x.(map[string]any)["id"].(float64))), nil)
	}
	if _, _, again := a.call(a.alice, "GET", "/api/v1/lenses", nil); len(again) != 1 {
		t.Fatalf("after deleting the defaults: %d lenses", len(again))
	}
}

func TestInstantAnswersInSearch(t *testing.T) {
	a := newApp(t)
	m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("15% of 240"), nil)
	inst, _ := m["instant"].(map[string]any)
	if inst == nil || inst["value"] != "36" || inst["kind"] != "calc" {
		t.Fatalf("instant = %v", m["instant"])
	}
	if m := a.ok(a.alice, "GET", "/api/v1/search?q=nikola+tesla", nil); m["instant"] != nil {
		t.Fatalf("instant for a plain search: %v", m["instant"])
	}
	// "my tasks today" reads the person's own Pocket Tasks
	tasks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/dashboard" {
			_, _ = io.WriteString(w, `{"today":2,"overdue":1,"upcoming_week":5,"next":[{"id":1,"title":"Call the plumber","due":"2020-01-01"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(tasks.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{"tasks": map[string]any{"mode": "direct", "url": tasks.URL, "token": "pt_x"}}})
	m = a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape("my tasks today"), nil)
	inst, _ = m["instant"].(map[string]any)
	if inst == nil || inst["value"] != "2 tasks due today" || inst["source"] != "Pocket Tasks" {
		t.Fatalf("tasks instant = %v", m["instant"])
	}
	if rows := inst["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["value"] != "Overdue" {
		t.Fatalf("rows = %v", inst["rows"])
	}
	// Bob has no tasks connection: no answer, just results
	if m := a.ok(a.bob, "GET", "/api/v1/search?q="+url.QueryEscape("my tasks today"), nil); m["instant"] != nil {
		t.Fatalf("bob's instant = %v", m["instant"])
	}
}

func TestItemURLKeepsPathSlashes(t *testing.T) {
	row := map[string]any{"space": "home", "path": "/My docs/report 1.pdf"}
	got := itemURL("https://drive.example.com/", "/f/{space}{path*}", row, []string{"path"})
	if want := "https://drive.example.com/f/home/My%20docs/report%201.pdf"; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	// ordinary fields still escape everything
	if got := itemURL("https://x", "/note/{id}", map[string]any{"id": "a/b"}, []string{"id"}); got != "https://x/note/a%2Fb" {
		t.Fatalf("got %s", got)
	}
}

// Pocket Blog's posts (drafts too) open at the post in Blog (/posts/{id}).
func TestYourAppsBlog(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"query":"sarajevo","results":[{"id":12,"title":"Spring in Sarajevo","snippet":"…the <b>river</b> in Sarajevo was loud","status":"published","kind":"post","date":"2026-11-06T09:30:00Z","url":"https://blog.example/spring","tags":["travel"],"link":"https://blog.example/posts/12"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"blog": map[string]any{"mode": "direct", "url": sib.URL, "token": "pbl_secret", "public": "https://blog.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=sarajevo&app=blog", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 1 || !strings.Contains(gotQuery, "q=sarajevo") || !strings.Contains(gotQuery, "limit=8") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	it := items[0].(map[string]any)
	if it["url"] != "https://blog.example/posts/12" || it["title"] != "Spring in Sarajevo" || it["kind"] != "post" || it["date"] != "2026-11-06T09:30:00Z" || strings.Contains(it["snippet"].(string), "<b>") {
		t.Fatalf("item = %v", it)
	}
}

// Pocket Chat's messages read "<who> in <conversation>" and open at the
// message in its conversation (/c/{conversation_id}?m={id}).
func TestYourAppsChat(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"query":"ferry","results":[{"id":"m41","conversation_id":"c7","kind":"text","body":"Let's take the early ferry to Hvar on Saturday","snippet":"…the early <b>ferry</b> to Hvar","author_name":"Ana","conversation_title":"Weekend plans","created_at":"2026-11-06T08:15:00Z"},{"id":"m42","conversation_id":"c8","kind":"text","body":"ferry tickets?","snippet":"<b>ferry</b> tickets?","author_name":"Marko","conversation_title":"","created_at":"2026-11-05T18:00:00Z"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"chat": map[string]any{"mode": "direct", "url": sib.URL, "token": "pcx_secret", "public": "https://chat.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=ferry&app=chat", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 2 || !strings.Contains(gotQuery, "q=ferry") || !strings.Contains(gotQuery, "limit=8") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	it := items[0].(map[string]any)
	if it["url"] != "https://chat.example/c/c7?m=m41" || it["title"] != "Ana in Weekend plans" || it["kind"] != "message" || it["date"] != "2026-11-06T08:15:00Z" || it["snippet"] != "…the early ferry to Hvar" {
		t.Fatalf("item = %v", it)
	}
	// no conversation title: the author alone
	if it := items[1].(map[string]any); it["title"] != "Marko" || it["url"] != "https://chat.example/c/c8?m=m42" {
		t.Fatalf("item = %v", it)
	}
}

// Pocket Forms' forms and answers: an answer reads "<who> — <form>" with its
// line and opens in the Forms inbox (/answers/{id}), a form in Forms (/forms/{id}).
func TestYourAppsForms(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"items":[{"kind":"form","id":2,"title":"Client brief","subtitle":"Form · 7 answers","url":"https://forms.example/forms/2"},{"kind":"response","id":41,"title":"Ana Kovač — Client brief","subtitle":"A new website for the bakery","date":"2026-11-07T09:12:00Z","url":"https://forms.example/answers/41"}],"forms":[{"id":2,"title":"Client brief"}],"responses":[{"id":41,"form":"Client brief"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"forms": map[string]any{"mode": "direct", "url": sib.URL, "token": "pfm_secret", "public": "https://forms.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=brief&app=forms", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 2 || !strings.Contains(gotQuery, "q=brief") || !strings.Contains(gotQuery, "limit=8") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	if it := items[0].(map[string]any); it["url"] != "https://forms.example/forms/2" || it["title"] != "Client brief" || it["kind"] != "form" || it["snippet"] != "Form · 7 answers" {
		t.Fatalf("form = %v", it)
	}
	if it := items[1].(map[string]any); it["url"] != "https://forms.example/answers/41" || it["title"] != "Ana Kovač — Client brief" || it["kind"] != "answer" || it["date"] != "2026-11-07T09:12:00Z" || it["snippet"] != "A new website for the bakery" {
		t.Fatalf("answer = %v", it)
	}
}

// Pocket CRM's deals: a deal reads its title with "<stage> · <amount> · <who>
// (<company>)" and opens in Pocket CRM (/deals/{id}).
func TestYourAppsCRM(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"deals":[{"id":4,"title":"Online shop for cakes","stage_name":"Proposal sent","status":"open","value":450000,"amount":4500,"currency":"EUR","contact_name":"Ana Marić","company":"Pekara Marić","next_action":"Call back","next_due":"2026-11-09","updated_at":"2026-11-07T09:12:00Z","url":"https://crm.example/deals/4"},{"id":9,"title":"Cake stand","stage_name":"Won","status":"won","amount":1234.5,"currency":"BAM","contact_name":"","company":"Pekara Marić","url":"https://crm.example/deals/9"},{"id":10,"title":"Bare deal","stage_name":"Lead","status":"open","amount":0,"currency":"EUR","url":"https://crm.example/deals/10"}],"total":3}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"crm": map[string]any{"mode": "direct", "url": sib.URL, "token": "pcr_secret", "public": "https://crm.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=cake&app=crm", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 3 || !strings.Contains(gotQuery, "q=cake") || !strings.Contains(gotQuery, "limit=8") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	if it := items[0].(map[string]any); it["url"] != "https://crm.example/deals/4" || it["title"] != "Online shop for cakes" || it["kind"] != "deal" || it["snippet"] != "Proposal sent · 4,500 EUR · Ana Marić (Pekara Marić)" || it["date"] != "2026-11-07T09:12:00Z" {
		t.Fatalf("deal = %v", it)
	}
	if it := items[1].(map[string]any); it["snippet"] != "Won · 1,234.50 BAM · Pekara Marić" || it["url"] != "https://crm.example/deals/9" {
		t.Fatalf("won deal = %v", it)
	}
	if it := items[2].(map[string]any); it["snippet"] != "Lead" {
		t.Fatalf("bare deal = %v", it)
	}
}

// Pocket CRM not connected: its section says so and the rest of the search goes on.
func TestYourAppsCRMOff(t *testing.T) {
	a := newApp(t)
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=cake&app=crm", nil)
	sec := m["sections"].([]any)[0].(map[string]any)
	if sec["app"] != "crm" || len(sec["items"].([]any)) != 0 || sec["error"] == nil || sec["error"] == "" {
		t.Fatalf("section = %v", sec)
	}
}

// Pocket League: groups, games, players and chat lines, each opening its own
// page in Pocket League; !league, !sport and !sports all search it.
func TestYourAppsLeague(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"results":[{"kind":"group","id":1,"title":"Tuesday Tennis","subtitle":"Tennis · 6 people","emoji":"🎾","url":"https://league.example/groups/1","path":"/groups/1"},{"kind":"game","id":31,"title":"Tennis · Tuesday Tennis","subtitle":"Tue 10 Nov, 20:00 · TK Koševo","emoji":"🎾","url":"https://league.example/games/31","path":"/games/31","at":"2026-11-10T20:00"},{"kind":"player","id":2,"title":"Marko Petrović","subtitle":"plays in Tuesday Tennis","emoji":"🎾","url":"https://league.example/groups/1/players/2","path":"/groups/1/players/2"},{"kind":"message","id":7,"title":"Tennis on Tuesday, who's bringing balls?","subtitle":"Marko Petrović in Tuesday Tennis","emoji":"🎾","url":"https://league.example/groups/1?tab=chat","path":"/groups/1?tab=chat","at":"2026-11-06T18:00:00Z"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"league": map[string]any{"mode": "direct", "url": sib.URL, "token": "plg_secret", "public": "https://league.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=tennis&app=league", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 4 || !strings.Contains(gotQuery, "q=tennis") || !strings.Contains(gotQuery, "limit=8") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	want := []struct{ kind, url, snippet string }{
		{"group", "https://league.example/groups/1", "Tennis · 6 people"},
		{"game", "https://league.example/games/31", "Tue 10 Nov, 20:00 · TK Koševo"},
		{"player", "https://league.example/groups/1/players/2", "plays in Tuesday Tennis"},
		{"message", "https://league.example/groups/1?tab=chat", "Marko Petrović in Tuesday Tennis"},
	}
	for i, w := range want {
		if it := items[i].(map[string]any); it["kind"] != w.kind || it["url"] != w.url || it["snippet"] != w.snippet {
			t.Fatalf("item %d = %v, want %+v", i, it, w)
		}
	}
	if it := items[1].(map[string]any); it["date"] != "2026-11-10T20:00" || it["title"] != "Tennis · Tuesday Tennis" {
		t.Fatalf("game = %v", it)
	}
	for _, b := range []string{"!league tennis", "!sport tennis", "tennis !sports"} {
		if m := a.ok(a.alice, "GET", "/api/v1/search?q="+url.QueryEscape(b), nil); m["redirect"] != "/search?cat=apps&app=league&q=tennis" {
			t.Fatalf("%s → %v", b, m["redirect"])
		}
	}
}

// Pocket League not connected: its section says so and the rest of the search goes on.
func TestYourAppsLeagueOff(t *testing.T) {
	a := newApp(t)
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=tennis&app=league", nil)
	sec := m["sections"].([]any)[0].(map[string]any)
	if sec["app"] != "league" || len(sec["items"].([]any)) != 0 || sec["error"] == nil || sec["error"] == "" {
		t.Fatalf("section = %v", sec)
	}
}

// Pocket Tables: tables whose name matches come first and open at /t/{id};
// rows open at /r/{id} and read "<table> in <base> · <field>: <snippet>".
func TestYourAppsTables(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"query":"client","items":[{"id":12,"title":"Ana Marić","table_id":3,"table":"Clients","table_icon":"👥","base":"Bakery","field":"Notes","snippet":"…our best client since 2024…","url":"https://tables.example/r/12","updated_at":"2026-11-07T09:12:00Z"},{"id":13,"title":"Client list","table_id":3,"table":"Clients","base":"Clients","url":"https://tables.example/r/13"}],"tables":[{"id":3,"name":"Clients","icon":"👥","base":"Bakery","rows":12,"url":"https://tables.example/t/3"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"tables": map[string]any{"mode": "direct", "url": sib.URL, "token": "ptb_secret", "public": "https://tables.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=client&app=tables", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 3 || !strings.Contains(gotQuery, "q=client") || !strings.Contains(gotQuery, "limit=8") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	if it := items[0].(map[string]any); it["url"] != "https://tables.example/t/3" || it["title"] != "Clients" || it["kind"] != "table" || it["snippet"] != "In Bakery · 12 rows" {
		t.Fatalf("table = %v", it)
	}
	if it := items[1].(map[string]any); it["url"] != "https://tables.example/r/12" || it["title"] != "Ana Marić" || it["kind"] != "row" || it["snippet"] != "Clients in Bakery · Notes: …our best client since 2024…" || it["date"] != "2026-11-07T09:12:00Z" {
		t.Fatalf("row = %v", it)
	}
	if it := items[2].(map[string]any); it["snippet"] != "Clients" || it["url"] != "https://tables.example/r/13" {
		t.Fatalf("name match = %v", it)
	}
	// !tables and !table both search it
	found := map[string]bool{}
	for _, b := range appBangs() {
		if b.Trigger == "tables" || b.Trigger == "table" {
			found[b.Trigger] = b.URL == "/search?cat=apps&app=tables&q={q}"
		}
	}
	if !found["tables"] || !found["table"] {
		t.Fatalf("bangs = %v", found)
	}
}

// Pocket Tables not connected: its section says so and the rest of the search goes on.
func TestYourAppsTablesOff(t *testing.T) {
	a := newApp(t)
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=client&app=tables", nil)
	sec := m["sections"].([]any)[0].(map[string]any)
	if sec["app"] != "tables" || len(sec["items"].([]any)) != 0 || sec["error"] == nil || sec["error"] == "" {
		t.Fatalf("section = %v", sec)
	}
}

// Pocket Rentals: places, leases, tenants and repairs each open their own page
// in Pocket Rentals and read Rentals' subtitle.
func TestYourAppsRentals(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"results":[{"kind":"property","id":2,"title":"Obala 12","subtitle":"Obala Kulina bana 12, Sarajevo · 2 of 3 let","url":"https://rentals.example/properties/2","path":"/properties/2"},{"kind":"lease","id":3,"title":"Flat 2 — Ana Marić","subtitle":"active · €650.00 monthly · owes €500.00","url":"https://rentals.example/leases/3","path":"/leases/3"},{"kind":"tenant","id":5,"title":"Ana Marić","subtitle":"ana@example.org · +387 61 000 000","url":"https://rentals.example/tenants/5","path":"/tenants/5"},{"kind":"repair","id":9,"title":"Leaking tap","subtitle":"Flat 1, Obala 12 · in progress","url":"https://rentals.example/repairs/9"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"rentals": map[string]any{"mode": "direct", "url": sib.URL, "token": "prt_secret", "public": "https://rentals.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=obala&app=rentals", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 4 || !strings.Contains(gotQuery, "q=obala") || !strings.Contains(gotQuery, "limit=8") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	want := []struct{ kind, url, snippet string }{
		{"property", "https://rentals.example/properties/2", "Obala Kulina bana 12, Sarajevo · 2 of 3 let"},
		{"lease", "https://rentals.example/leases/3", "active · €650.00 monthly · owes €500.00"},
		{"tenant", "https://rentals.example/tenants/5", "ana@example.org · +387 61 000 000"},
		{"repair", "https://rentals.example/repairs/9", "Flat 1, Obala 12 · in progress"}, // no path: from its kind
	}
	for i, w := range want {
		it := items[i].(map[string]any)
		if it["kind"] != w.kind || it["url"] != w.url || it["snippet"] != w.snippet {
			t.Errorf("item %d = %v", i, it)
		}
	}
	// !rentals and !rent both search it
	found := map[string]bool{}
	for _, b := range appBangs() {
		if b.Trigger == "rentals" || b.Trigger == "rent" {
			found[b.Trigger] = b.URL == "/search?cat=apps&app=rentals&q={q}"
		}
	}
	if !found["rentals"] || !found["rent"] {
		t.Fatalf("bangs = %v", found)
	}
}

// Pocket Rentals not connected: its section says so and the rest of the search goes on.
func TestYourAppsRentalsOff(t *testing.T) {
	a := newApp(t)
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=obala&app=rentals", nil)
	sec := m["sections"].([]any)[0].(map[string]any)
	if sec["app"] != "rentals" || len(sec["items"].([]any)) != 0 || sec["error"] == nil || sec["error"] == "" {
		t.Fatalf("section = %v", sec)
	}
}

// Pocket Trails: routes, outings, gear and spots each open their page in
// Pocket Trails (a spot its route) and read Trails' subtitle.
func TestYourAppsTrails(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"results":[{"type":"route","id":4,"title":"Trebević from Sarajevo","subtitle":"Hike · 12.4 km · 980 m up · 4 h 30 min","path":"/routes/4","url":"https://trails.example/routes/4"},{"type":"outing","id":12,"title":"Hike: Trebević from Sarajevo","subtitle":"Sat 14 Nov · planned","date":"2026-11-14","path":"/outings/12","url":"https://trails.example/outings/12"},{"type":"gear","id":3,"title":"Trebević map","subtitle":"Gear · 80 g","path":"/gear","url":"https://trails.example/gear"},{"type":"spot","id":7,"title":"Trebević hut","subtitle":"Hut on a route"},{"type":"outing","id":13,"title":"Run by the river","subtitle":"Sun 1 Nov · done"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"trails": map[string]any{"mode": "direct", "url": sib.URL, "token": "ptr_secret", "public": "https://trails.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=trebevic&app=trails", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 5 || !strings.Contains(gotQuery, "q=trebevic") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	want := []struct{ kind, url, snippet, date string }{
		{"route", "https://trails.example/routes/4", "Hike · 12.4 km · 980 m up · 4 h 30 min", ""},
		{"outing", "https://trails.example/outings/12", "Sat 14 Nov · planned", "2026-11-14"},
		{"gear", "https://trails.example/gear", "Gear · 80 g", ""},
		{"spot", "https://trails.example/", "Hut on a route", ""},               // no path: its route is not known
		{"outing", "https://trails.example/outings/13", "Sun 1 Nov · done", ""}, // no path: from its type
	}
	for i, w := range want {
		it := items[i].(map[string]any)
		if it["kind"] != w.kind || it["url"] != w.url || it["snippet"] != w.snippet || (w.date != "" && it["date"] != w.date) {
			t.Errorf("item %d = %v", i, it)
		}
	}
	// !trails and !hike both search it
	found := map[string]bool{}
	for _, b := range appBangs() {
		if b.Trigger == "trails" || b.Trigger == "hike" {
			found[b.Trigger] = b.URL == "/search?cat=apps&app=trails&q={q}"
		}
	}
	if !found["trails"] || !found["hike"] {
		t.Fatalf("bangs = %v", found)
	}
}

// Pocket Trails not connected: its section says so and the rest of the search goes on.
func TestYourAppsTrailsOff(t *testing.T) {
	a := newApp(t)
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=trebevic&app=trails", nil)
	sec := m["sections"].([]any)[0].(map[string]any)
	if sec["app"] != "trails" || len(sec["items"].([]any)) != 0 || sec["error"] == nil || sec["error"] == "" {
		t.Fatalf("section = %v", sec)
	}
}

// Pocket Karaoke: songs (by title, artist or a line of their words), singers
// and nights each open their page in Pocket Karaoke; a song found by its
// words shows the artist and the line.
func TestYourAppsKaraoke(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"hits":[{"kind":"song","id":1,"title":"Dancing Queen","subtitle":"ABBA","snippet":"","url":"https://karaoke.example/songs/1","path":"/songs/1"},{"kind":"song","id":2,"title":"Waterloo","subtitle":"ABBA","snippet":"“My, my, at Waterloo Napoleon did surrender”","path":"/songs/2"},{"kind":"singer","id":3,"title":"Ana","subtitle":"12 songs sung","path":"/singers/3"},{"kind":"night","id":4,"title":"Friday karaoke","subtitle":"2026-11-13 21:00 Living room"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"karaoke": map[string]any{"mode": "direct", "url": sib.URL, "token": "pka_secret", "public": "https://karaoke.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=abba&app=karaoke", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 4 || !strings.Contains(gotQuery, "q=abba") || !strings.Contains(gotQuery, "limit=8") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	want := []struct{ kind, title, url, snippet string }{
		{"song", "Dancing Queen", "https://karaoke.example/songs/1", "ABBA"},
		{"song", "Waterloo", "https://karaoke.example/songs/2", "ABBA · “My, my, at Waterloo Napoleon did surrender”"},
		{"singer", "Ana", "https://karaoke.example/singers/3", "12 songs sung"},
		{"night", "Friday karaoke", "https://karaoke.example/nights/4", "2026-11-13 21:00 Living room"}, // no path: from its kind
	}
	for i, w := range want {
		it := items[i].(map[string]any)
		if it["kind"] != w.kind || it["title"] != w.title || it["url"] != w.url || it["snippet"] != w.snippet {
			t.Errorf("item %d = %v", i, it)
		}
	}
	// !karaoke and !sing both search it
	found := map[string]bool{}
	for _, b := range appBangs() {
		if b.Trigger == "karaoke" || b.Trigger == "sing" {
			found[b.Trigger] = b.URL == "/search?cat=apps&app=karaoke&q={q}"
		}
	}
	if !found["karaoke"] || !found["sing"] {
		t.Fatalf("bangs = %v", found)
	}
}

// Pocket Karaoke not connected: its section says so and the rest of the search goes on.
func TestYourAppsKaraokeOff(t *testing.T) {
	a := newApp(t)
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=abba&app=karaoke", nil)
	sec := m["sections"].([]any)[0].(map[string]any)
	if sec["app"] != "karaoke" || len(sec["items"].([]any)) != 0 || sec["error"] == nil || sec["error"] == "" {
		t.Fatalf("section = %v", sec)
	}
}

// Pocket Print Farm: models, printers, filament, prints and jobs each open
// their page in Pocket Print Farm.
func TestYourAppsPrintFarm(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"hits":[{"kind":"model","id":2,"title":"Twisted vase","subtitle":"home, vase mode · 52 printed","path":"/models/2"},{"kind":"printer","id":1,"title":"Mk4","subtitle":"Prusa MK4 · Workshop","path":"/printers/1"},{"kind":"print","id":9,"title":"Twisted vase","subtitle":"2026-11-10 · Mk4 · done"},{"kind":"job","id":4,"title":"Vase for Ana","subtitle":"in the queue, 0 of 1","path":"/queue"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"printfarm": map[string]any{"mode": "direct", "url": sib.URL, "token": "ppf_secret", "public": "https://printfarm.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=vase&app=printfarm", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 4 || !strings.Contains(gotQuery, "q=vase") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	want := []struct{ kind, url string }{
		{"model", "https://printfarm.example/models/2"},
		{"printer", "https://printfarm.example/printers/1"},
		{"print", "https://printfarm.example/prints/9"}, // no path: from its kind
		{"queued", "https://printfarm.example/queue"},
	}
	for i, w := range want {
		it := items[i].(map[string]any)
		if it["kind"] != w.kind || it["url"] != w.url {
			t.Errorf("item %d = %v", i, it)
		}
	}
	found := false
	for _, b := range appBangs() {
		if b.Trigger == "3d" {
			found = b.URL == "/search?cat=apps&app=printfarm&q={q}"
		}
	}
	if !found {
		t.Fatal("!3d does not search Pocket Print Farm")
	}
}

// Pocket Game Builder: a game opens its page, a picture, sound or music piece
// the library; !gamebuilder and !mygames search it.
func TestYourAppsGameBuilder(t *testing.T) {
	a := newApp(t)
	var gotQuery string
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"results":[{"kind":"game","id":3,"title":"Cat and fish","subtitle":"catch · shared","text":"a cat catching fish","url":"https://gamebuilder.example/games/3","date":"2026-11-10"},{"kind":"image","id":7,"title":"Fish","subtitle":"picture","url":"https://gamebuilder.example/library","date":"2026-11-09"},{"kind":"sound","id":8,"title":"Splash","subtitle":"sound effect"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"gamebuilder": map[string]any{"mode": "direct", "url": sib.URL, "token": "pgb_secret", "public": "https://gamebuilder.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=fish&app=gamebuilder", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 3 || !strings.Contains(gotQuery, "q=fish") {
		t.Fatalf("items = %v (query %q)", items, gotQuery)
	}
	want := []struct{ kind, url string }{
		{"game", "https://gamebuilder.example/games/3"},
		{"picture", "https://gamebuilder.example/library"},
		{"sound", "https://gamebuilder.example/library"},
	}
	for i, w := range want {
		it := items[i].(map[string]any)
		if it["kind"] != w.kind || it["url"] != w.url {
			t.Errorf("item %d = %v", i, it)
		}
	}
	found := 0
	for _, b := range appBangs() {
		if (b.Trigger == "gamebuilder" || b.Trigger == "mygames") && b.URL == "/search?cat=apps&app=gamebuilder&q={q}" {
			found++
		}
	}
	if found != 2 {
		t.Fatal("!gamebuilder and !mygames do not both search Pocket Game Builder")
	}
}

// Pocket Sky: objects of the sky open on its star map, nights on its log; !sky searches it.
func TestYourAppsSky(t *testing.T) {
	a := newApp(t)
	sib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"results":[{"id":"saturn","kind":"planet","title":"Saturn","snippet":"Planet. The ringed planet","url":"/map?object=saturn"},{"id":"log:3","kind":"observation","title":"Night of 2026-11-01","snippet":"Saturn, M31","date":"2026-11-01","url":"/log/3"}]}`)
	}))
	t.Cleanup(sib.Close)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"apps": map[string]any{
		"sky": map[string]any{"mode": "direct", "url": sib.URL, "token": "psky_secret", "public": "https://sky.example"},
	}})
	m := a.ok(a.alice, "GET", "/api/v1/search/apps?q=saturn&app=sky", nil)
	items := m["sections"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v", items)
	}
	first := items[0].(map[string]any)
	if !strings.Contains(fmt.Sprint(first["url"]), "sky.example/map?object=saturn") || first["kind"] != "planet" {
		t.Fatalf("first = %v", first)
	}
	if second := items[1].(map[string]any); second["kind"] != "night logged" {
		t.Fatalf("second = %v", second)
	}
}
