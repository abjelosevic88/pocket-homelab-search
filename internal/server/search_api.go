package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abjelosevic88/pocket-homelab-search/internal/engine"
	"github.com/abjelosevic88/pocket-homelab-search/internal/instant"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// ---- web search ----

// categories a person can search in (SearXNG's names); "apps" is Pocket
// Search's own: only the person's Pocket apps.
var categories = map[string]string{
	"general": "All", "images": "Pictures", "videos": "Videos", "news": "News",
	"music": "Music", "it": "Code", "science": "Science", "files": "Files", "map": "Maps", "social media": "Social",
}

var searchesRun atomic.Int64

var timeRanges = map[string]bool{"": true, "day": true, "week": true, "month": true, "year": true}

// hit is one result as the person sees it.
type hit struct {
	engine.Result
	Domain string       `json:"domain"`
	Rule   string       `json:"rule,omitempty"` // pin | boost | lower: why it moved
	Saved  []store.Save `json:"saved"`          // where the person already sent it
}

type searchOut struct {
	Query        string                `json:"query"`
	Category     string                `json:"category"`
	Page         int                   `json:"page"`
	Time         string                `json:"time"`
	Results      []hit                 `json:"results"`
	Answers      []engine.Answer       `json:"answers"`
	Infoboxes    []engine.Infobox      `json:"infoboxes"`
	Suggestions  []string              `json:"suggestions"`
	Corrections  []string              `json:"corrections"`
	Unresponsive []engine.Unresponsive `json:"unresponsive"`
	Hidden       int                   `json:"hidden"` // results your rules hid
	HiddenSites  []string              `json:"hidden_sites"`
	More         bool                  `json:"more"` // there is probably a next page
	Engine       string                `json:"engine"`
	MS           int64                 `json:"ms"`
	Redirect     string                `json:"redirect,omitempty"`     // a bang: go here instead
	Instant      *instant.Answer       `json:"instant,omitempty"`      // worked out on the server: a sum, the weather…
	Pending      bool                  `json:"pending,omitempty"`      // slower engines are still answering: ask again with full=1
	CardPending  bool                  `json:"card_pending,omitempty"` // more facts for the card on their way: ask again with card=1
}

// how long a search waits for its engines
const (
	waitFirst = iota // the first one with results
	waitAll          // all but the late ones (Wikidata)
	waitCard         // all of them
)

// domainOf is a result's site without "www.".
func domainOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// ruleFor finds the rule for a site (the most specific domain wins).
func ruleFor(rules []store.SiteRule, domain string) *store.SiteRule {
	var best *store.SiteRule
	for i := range rules {
		d := rules[i].Domain
		if domain == d || strings.HasSuffix(domain, "."+d) {
			if best == nil || len(d) > len(best.Domain) {
				best = &rules[i]
			}
		}
	}
	return best
}

// applyRules orders results by a person's site rules: pinned sites first,
// then raised ones, then the rest, then lowered ones; blocked sites go.
// Within each group the engine's order stays.
func applyRules(in []engine.Result, rules []store.SiteRule) (out []hit, hidden int, hiddenSites []string, hits map[int64]int) {
	type ranked struct {
		h    hit
		band int
		i    int
	}
	hits = map[int64]int{}
	seenHidden := map[string]bool{}
	var rs []ranked
	for i, r := range in {
		h := hit{Result: r, Domain: domainOf(r.URL), Saved: []store.Save{}}
		if h.Engines == nil {
			h.Engines = []string{}
		}
		band := 2
		if rule := ruleFor(rules, h.Domain); rule != nil {
			hits[rule.ID]++
			switch rule.Action {
			case store.RuleBlock:
				hidden++
				if !seenHidden[rule.Domain] {
					seenHidden[rule.Domain] = true
					hiddenSites = append(hiddenSites, rule.Domain)
				}
				continue
			case store.RulePin:
				band = 0
			case store.RuleBoost:
				band = 1
			case store.RuleLower:
				band = 3
			}
			if band != 2 {
				h.Rule = rule.Action
			}
		}
		rs = append(rs, ranked{h, band, i})
	}
	sort.SliceStable(rs, func(a, b int) bool { return rs[a].band < rs[b].band })
	out = make([]hit, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.h)
	}
	if hiddenSites == nil {
		hiddenSites = []string{}
	}
	return out, hidden, hiddenSites, hits
}

// engineList is SearXNG's engine list, kept for ten minutes.
func (s *Server) engineList(ctx context.Context) ([]engine.Info, error) {
	s.engMu.Lock()
	defer s.engMu.Unlock()
	if s.engInfo != nil && time.Since(s.engAt) < 10*time.Minute {
		return s.engInfo, nil
	}
	list, err := s.engine.Engines(ctx)
	if err != nil {
		if s.engInfo != nil {
			return s.engInfo, nil
		}
		return nil, err
	}
	s.engInfo, s.engAt = list, time.Now()
	return list, nil
}

// lastSearch keeps a person's latest result pages a few minutes, so an AI
// answer or a second look does not ask every engine again.
type cachedSearch struct {
	out *searchOut
	at  time.Time
}

func searchKey(uid int64, q, cat, tr string, page int) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%d", uid, strings.ToLower(strings.TrimSpace(q)), cat, tr, page)
}

func (s *Server) cached(key string) *searchOut {
	s.resMu.Lock()
	defer s.resMu.Unlock()
	if c, ok := s.results[key]; ok && time.Since(c.at) < 5*time.Minute {
		return c.out
	}
	return nil
}

func (s *Server) remember(key string, out *searchOut) {
	s.resMu.Lock()
	defer s.resMu.Unlock()
	for k, c := range s.results {
		if time.Since(c.at) > 5*time.Minute {
			delete(s.results, k)
		}
	}
	if len(s.results) > 500 {
		s.results = map[string]cachedSearch{}
	}
	s.results[key] = cachedSearch{out, time.Now()}
}

// search runs one search for a person with their settings and rules, back
// as soon as the engines wait asks for have answered (Pending / CardPending
// then say more is on its way).
func (s *Server) search(ctx context.Context, u *store.User, st store.Settings, q, cat, tr, lang string, page int, lens *store.Lens, wait int) (*searchOut, error) {
	lensKey := ""
	if lens != nil {
		lensKey = fmt.Sprintf("%d@%s", lens.ID, lens.UpdatedAt)
	}
	key := searchKey(u.ID, q, cat, tr+"\x00"+lang+"\x00"+lensKey, page)
	out := s.cached(key)
	if wait == waitCard && out != nil && out.CardPending {
		out = nil // the card's late facts are still on their way: wait for them below
	}
	if out == nil {
		if !s.engine.Configured() {
			return nil, engine.ErrNotConfigured
		}
		// the same search already on its way (a tab hovered, then clicked): wait for that one
		s.resMu.Lock()
		job, ok := s.inflight[key]
		if !ok {
			job = &searchJob{early: make(chan struct{}), done: make(chan struct{}), all: make(chan struct{})}
			s.inflight[key] = job
			go s.runSearch(job, key, u.ID, st, q, cat, tr, lang, page, lens)
		}
		s.resMu.Unlock()
		ready := job.early
		switch wait {
		case waitAll:
			ready = job.done
		case waitCard:
			ready = job.all
		}
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if job.err != nil {
			return nil, job.err
		}
		out = job.first
		if wait >= waitAll && job.final != nil {
			out = job.final
		}
		if wait == waitCard && job.card != nil {
			out = job.card
		}
		if out == nil {
			return nil, errors.New("the search engine did not answer")
		}
	}
	cp := *out
	cp.Results = append(make([]hit, 0, len(out.Results)), out.Results...)
	s.attachSaves(ctx, u.ID, cp.Results)
	return &cp, nil
}

func (s *Server) engineName() string {
	if _, ok := s.engine.(engine.Demo); ok {
		return "demo"
	}
	return "searxng"
}

func (s *Server) attachSaves(ctx context.Context, uid int64, hits []hit) {
	urls := make([]string, 0, len(hits))
	for _, h := range hits {
		urls = append(urls, h.URL)
	}
	saves, err := s.store.SavesFor(ctx, uid, urls)
	if err != nil {
		return
	}
	for i := range hits {
		hits[i].Saved = saves[hits[i].URL]
		if hits[i].Saved == nil {
			hits[i].Saved = []store.Save{}
		}
	}
}

// handleSearch: GET /api/v1/search?q=&cat=&page=&time=&lang=
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		// Pocket Core's AI tool names it "terms", so cross-app searches of
		// the person's own apps do not also search the web
		q = strings.TrimSpace(r.URL.Query().Get("terms"))
	}
	if q == "" {
		writeErr(w, http.StatusBadRequest, "type something to search for")
		return
	}
	if len(q) > 500 {
		writeErr(w, http.StatusBadRequest, "that search is too long (500 characters at most)")
		return
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	if to := s.resolveBang(ctx, u.ID, q); to != "" {
		writeJSON(w, http.StatusOK, searchOut{Query: q, Redirect: to, Results: []hit{}})
		return
	}
	cat := r.URL.Query().Get("cat")
	if cat == "" {
		cat = st.DefaultCategory
	}
	if _, ok := categories[cat]; !ok {
		writeErr(w, http.StatusBadRequest, "unknown category")
		return
	}
	tr := r.URL.Query().Get("time")
	if !timeRanges[tr] {
		writeErr(w, http.StatusBadRequest, "time is day, week, month or year")
		return
	}
	page := max(1, min(queryInt(r, "page"), 20))
	var lens *store.Lens
	if id := int64(queryInt(r, "lens")); id > 0 {
		l, err := s.store.GetLens(ctx, u.ID, id)
		if err != nil {
			writeErr(w, http.StatusNotFound, "that lens is gone")
			return
		}
		lens = l
	}
	// full=1: the rest of a search whose first answer is already shown; other
	// apps and assistants always get every engine's answer at once
	full := queryBool(r, "full") || linkApp(ctx) != "" || r.URL.Query().Get("terms") != ""
	// card=1: the late engines' facts for the card, after the rest
	card := queryBool(r, "card")
	wait := waitFirst
	if full {
		wait = waitAll
	}
	if card {
		wait, full = waitCard, true
	}
	// instant answers (a sum, a conversion, the weather…) are worked out
	// while the engines search
	var inst *instant.Answer
	instDone := make(chan struct{})
	go func() {
		defer close(instDone)
		if page == 1 && cat == "general" && lens == nil && !full {
			ictx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			inst = s.instantAnswer(ictx, u, st, q)
		}
	}()
	out, err := s.search(ctx, u, st, q, cat, tr, r.URL.Query().Get("lang"), page, lens, wait)
	<-instDone
	if err != nil {
		if errors.Is(err, engine.ErrNotConfigured) {
			writeErr(w, http.StatusServiceUnavailable, "There is no search engine yet: start the bundled one (docker compose) or set SEARXNG_URL. Your Pocket apps can still be searched.")
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	// another app or an assistant searching for the person leaves no history
	if page == 1 && !full && st.History && linkApp(ctx) == "" && r.URL.Query().Get("terms") == "" && !queryBool(r, "nohistory") {
		_ = s.store.AddHistory(ctx, u.ID, &store.HistoryEntry{Kind: store.KindSearch, Query: q, Category: cat, Results: len(out.Results)})
	}
	out.Instant = inst
	writeJSON(w, http.StatusOK, out)
}

// handleSuggest: GET /api/v1/suggest?q= — earlier searches, the engine's
// suggestions and matching bangs. format=opensearch answers browsers.
func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	q := r.URL.Query().Get("q")
	st, _ := s.store.GetSettings(ctx, u.ID)
	out := map[string]any{"history": []string{}, "suggestions": []string{}, "bangs": []bang{}}
	trimmed := strings.TrimSpace(q)
	if strings.HasPrefix(trimmed, "!") && !strings.Contains(trimmed, " ") {
		pre := strings.ToLower(strings.TrimPrefix(trimmed, "!"))
		var bs []bang
		for _, b := range s.allBangs(ctx, u.ID) {
			if strings.HasPrefix(b.Trigger, pre) {
				bs = append(bs, b)
			}
		}
		sort.SliceStable(bs, func(i, j int) bool { return len(bs[i].Trigger) < len(bs[j].Trigger) })
		out["bangs"] = bs[:min(len(bs), 8)]
		writeJSON(w, http.StatusOK, out)
		return
	}
	var hist, sugg []string
	var wg sync.WaitGroup
	if st.History {
		hist, _ = s.store.RecentQueries(ctx, u.ID, trimmed, 4)
	}
	if st.Suggestions && trimmed != "" && s.engine.Configured() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			sugg, _ = s.engine.Suggest(sctx, trimmed, st.Language)
		}()
	}
	wg.Wait()
	seen := map[string]bool{strings.ToLower(trimmed): true}
	dedupe := func(list []string, n int) []string {
		res := []string{}
		for _, v := range list {
			k := strings.ToLower(strings.TrimSpace(v))
			if k == "" || seen[k] || len(res) >= n {
				continue
			}
			seen[k] = true
			res = append(res, v)
		}
		return res
	}
	h := dedupe(hist, 4)
	sg := dedupe(sugg, 8-len(h))
	if r.URL.Query().Get("format") == "opensearch" {
		w.Header().Set("Content-Type", "application/x-suggestions+json")
		_ = json.NewEncoder(w).Encode([]any{q, append(h, sg...)})
		return
	}
	out["history"], out["suggestions"] = h, sg
	writeJSON(w, http.StatusOK, out)
}

// handleSuggestMaybeSignedIn serves suggestions to a signed-in person, and to
// a browser's address bar that asks without the login cookie (OpenSearch
// format only): then only the engine's suggestions — nothing personal.
func (s *Server) handleSuggestMaybeSignedIn(w http.ResponseWriter, r *http.Request) {
	u, app, err := s.resolveAuth(r)
	if errors.Is(err, errLinkScope) {
		writeErr(w, http.StatusForbidden, "this Pocket Core link does not cover "+r.Method+" "+r.URL.Path)
		return
	}
	if err == nil && u != nil {
		ctx := context.WithValue(r.Context(), userKey, u)
		if app != "" {
			ctx = context.WithValue(ctx, linkAppKey, app)
		}
		s.handleSuggest(w, r.WithContext(ctx))
		return
	}
	if r.URL.Query().Get("format") != "opensearch" {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := []string{}
	if q != "" && len(q) <= 200 && !strings.HasPrefix(q, "!") && s.engine.Configured() {
		sctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if list, err := s.engine.Suggest(sctx, q, ""); err == nil {
			out = list[:min(len(list), 8)]
		}
	}
	w.Header().Set("Content-Type", "application/x-suggestions+json")
	w.Header().Set("Cache-Control", "private, max-age=60")
	_ = json.NewEncoder(w).Encode([]any{r.URL.Query().Get("q"), out})
}

// handleEngines: GET /api/v1/engines — the engines per category and whether
// this person uses each one.
func (s *Server) handleEngines(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.engine.Configured() {
		writeErr(w, http.StatusServiceUnavailable, "there is no search engine yet")
		return
	}
	list, err := s.engineList(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	off, on := map[string]bool{}, map[string]bool{}
	for _, n := range st.EnginesOff {
		off[n] = true
	}
	for _, n := range st.EnginesOn {
		on[n] = true
	}
	type row struct {
		engine.Info
		On bool `json:"on"`
	}
	out := map[string][]row{}
	for _, cat := range []string{"general", "images", "videos", "news", "music", "it", "science", "files", "map", "social media"} {
		out[cat] = []row{}
	}
	for _, e := range list {
		use := (e.Enabled && !off[e.Name]) || (!e.Enabled && on[e.Name])
		for _, c := range e.Categories {
			if _, ok := out[c]; ok {
				out[c] = append(out[c], row{e, use})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": categories, "engines": out})
}

// handleBangRedirect serves /search for browsers (the search-engine address):
// a bang jumps at once, everything else is the app's results page.
func (s *Server) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		if u, err := s.resolveUser(r); err == nil && u != nil {
			if to := s.resolveBang(r.Context(), u.ID, q); to != "" {
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, to, http.StatusFound)
				return
			}
		}
	}
	s.handleSPA(w, r)
}

// handleOpenSearch lets browsers add Pocket Search as a search engine.
func (s *Server) handleOpenSearch(w http.ResponseWriter, r *http.Request) {
	base := s.cfg.BaseURL
	if base == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	st, _ := s.store.GetSettings(r.Context(), 0)
	name := xmlEscape(st.Title)
	w.Header().Set("Content-Type", "application/opensearchdescription+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/" xmlns:moz="http://www.mozilla.org/2006/browser/search/">
  <ShortName>%s</ShortName>
  <Description>Private search: the web through your own server, plus your Pocket apps.</Description>
  <InputEncoding>UTF-8</InputEncoding>
  <Image width="64" height="64" type="image/svg+xml">%s/favicon.svg</Image>
  <Url type="text/html" method="get" template="%s/search?q={searchTerms}"/>
  <Url type="application/x-suggestions+json" method="get" template="%s/api/v1/suggest?format=opensearch&amp;q={searchTerms}"/>
  <Url type="application/opensearchdescription+xml" rel="self" template="%s/opensearch.xml"/>
  <moz:SearchForm>%s/</moz:SearchForm>
</OpenSearchDescription>
`, name, base, base, base, base, base)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// ---- history ----

func (s *Server) handleListHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	list, total, err := s.store.ListHistory(ctx, actorID(ctx), store.HistoryFilter{Q: q.Get("q"), Kind: q.Get("kind"), Limit: queryInt(r, "limit"), Offset: queryInt(r, "offset")})
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": total})
}

func (s *Server) handleDeleteHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteHistory(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "history")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleClearHistory: POST /history/clear {"range": "hour"|"day"|"week"|"all"}
func (s *Server) handleClearHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Range string `json:"range"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	since := ""
	switch in.Range {
	case "hour":
		since = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	case "day":
		since = time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	case "week":
		since = time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339Nano)
	case "all", "":
	default:
		writeErr(w, http.StatusBadRequest, "range is hour, day, week or all")
		return
	}
	n, err := s.store.ClearHistory(ctx, actorID(ctx), since)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": n})
}

// handleOpened remembers a result the person opened (when history is on).
func (s *Server) handleOpened(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		Query string `json:"query"`
	}
	if err := decode(r, &in); err != nil || in.URL == "" {
		writeErr(w, http.StatusBadRequest, "url is required")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	if st.History {
		_ = s.store.AddHistory(ctx, uid, &store.HistoryEntry{Kind: store.KindOpen, URL: oneLine(in.URL, 2000), Title: oneLine(in.Title, 300), Query: oneLine(in.Query, 500)})
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- site rules ----

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListRules(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "rules")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func validRule(rule *store.SiteRule) string {
	rule.Domain = store.NormDomain(rule.Domain)
	rule.Note = strings.TrimSpace(rule.Note)
	if rule.Domain == "" || !strings.Contains(rule.Domain, ".") || strings.ContainsAny(rule.Domain, " /") {
		return "Enter a site like example.com"
	}
	if !store.ValidRule(rule.Action) {
		return "A rule pins, raises, lowers or hides a site"
	}
	return ""
}

// handleSetRule adds a rule, or changes the one the site already has.
func (s *Server) handleSetRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in store.SiteRule
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := validRule(&in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.SetRule(ctx, actorID(ctx), &in); err != nil {
		s.fail(w, err, "rule")
		return
	}
	s.forgetResults(actorID(ctx))
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	cur, err := s.store.GetRule(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "rule")
		return
	}
	keys, err := decodeKeys(r, cur)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	_ = keys
	cur.ID = idParam(r, "id")
	if msg := validRule(cur); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateRule(ctx, uid, cur); err != nil {
		s.fail(w, err, "rule")
		return
	}
	s.forgetResults(uid)
	out, _ := s.store.GetRule(ctx, uid, cur.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteRule(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "rule")
		return
	}
	s.forgetResults(actorID(ctx))
	w.WriteHeader(http.StatusNoContent)
}

// forgetResults drops a person's remembered results (their rules changed).
func (s *Server) forgetResults(uid int64) {
	s.resMu.Lock()
	defer s.resMu.Unlock()
	pre := fmt.Sprintf("%d\x00", uid)
	for k := range s.results {
		if strings.HasPrefix(k, pre) {
			delete(s.results, k)
		}
	}
}

// ---- bangs ----

func (s *Server) handleListBangs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mine, err := s.store.ListBangs(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "bangs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mine": mine, "builtin": append(append([]bang{}, builtinBangs...), appBangs()...)})
}

func (s *Server) handleCreateBang(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in store.Bang
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := validBang(&in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateBang(ctx, actorID(ctx), &in); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "you already have !"+in.Trigger)
			return
		}
		s.fail(w, err, "bang")
		return
	}
	writeJSON(w, http.StatusCreated, in)
}

func (s *Server) handleUpdateBang(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in store.Bang
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.ID = idParam(r, "id")
	if msg := validBang(&in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateBang(ctx, actorID(ctx), &in); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "you already have !"+in.Trigger)
			return
		}
		s.fail(w, err, "bang")
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) handleDeleteBang(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteBang(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "bang")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
