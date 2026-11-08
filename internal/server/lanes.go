package server

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-search/internal/engine"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// A page of results no longer waits for its slowest engine: every engine is
// asked on its own (a lane), the first one with results is shown at once and
// the others' new results are added below as they come in, never moving what
// is already on screen. The card about the thing (Wikipedia/Wikidata, often
// the slowest of all) is a lane of its own and never holds the results up.

// factEngines fill the side card rather than the results.
var factEngines = map[string]bool{"wikipedia": true, "wikidata": true}

// laneGrace is how long the first answer waits for the others, so engines
// answering nearly together are ranked together.
const laneGrace = 150 * time.Millisecond

// maxLanes keeps a category with many engines from asking SearXNG dozens of
// times at once: the engines past it share one lane.
const maxLanes = 12

// searchJob is one search on its way: early is closed when there is
// something to show, done when every engine has answered.
type searchJob struct {
	early, done chan struct{}
	earlyOnce   sync.Once
	first       *searchOut // what was shown first
	final       *searchOut // everything: first, then what came later below it
	err         error
}

func (j *searchJob) showEarly(out *searchOut, err error) {
	j.earlyOnce.Do(func() {
		j.first, j.err = out, err
		close(j.early)
	})
}

// lanesFor splits a category's engines into lanes; nil means one plain
// search with SearXNG's own defaults.
func (s *Server) lanesFor(ctx context.Context, st store.Settings, cat string, page int) ([][]string, error) {
	if _, ok := s.engine.(*engine.SearXNG); !ok {
		return nil, nil
	}
	all, err := s.engineList(ctx)
	if err != nil {
		return nil, nil
	}
	picked := engine.Pick(all, cat, st.EnginesOff, st.EnginesOn)
	if picked != nil && len(picked) == 0 {
		return nil, errors.New("every search engine for " + strings.ToLower(categories[cat]) + " is switched off (Settings → Engines)")
	}
	if picked == nil {
		for _, e := range all {
			if e.Enabled && has(e.Categories, cat) {
				picked = append(picked, e.Name)
			}
		}
		if len(picked) == 0 {
			return nil, nil
		}
	}
	var facts, web []string
	for _, n := range picked {
		if factEngines[n] {
			facts = append(facts, n)
		} else {
			web = append(web, n)
		}
	}
	// a next page has no side card, and asks once
	if page > 1 {
		if len(web) == 0 {
			return [][]string{picked}, nil
		}
		return [][]string{web}, nil
	}
	var lanes [][]string
	for i, n := range web {
		if i == maxLanes-1 && len(web) > maxLanes {
			lanes = append(lanes, web[i:])
			break
		}
		lanes = append(lanes, []string{n})
	}
	if len(facts) > 0 {
		lanes = append(lanes, facts)
	}
	return lanes, nil
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type laneAnswer struct {
	names []string
	facts bool
	res   *engine.Response
	err   error
}

// runSearch asks the engines for one search and fills the job; it runs on
// its own, so a search left half way still finishes for the next look.
func (s *Server) runSearch(job *searchJob, key string, uid int64, st store.Settings, q, cat, tr, lang string, page int, lens *store.Lens) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.EngineTimeout+3*time.Second)
	defer cancel()
	defer func() {
		s.resMu.Lock()
		delete(s.inflight, key)
		s.resMu.Unlock()
		close(job.done)
	}()
	if lang == "" && lens != nil && lens.Language != "" {
		lang = lens.Language
	}
	if lang == "" {
		lang = st.Language
	}
	base := engine.Query{Q: lensQuery(q, lens), Category: cat, Page: page, TimeRange: tr, Language: lang, SafeSearch: st.SafeSearch}
	lanes, err := s.lanesFor(ctx, st, cat, page)
	if err != nil {
		job.showEarly(nil, err)
		return
	}
	if lanes == nil {
		lanes = [][]string{nil}
	}
	start := time.Now()
	searchesRun.Add(1)
	answers := make(chan laneAnswer, len(lanes))
	webLanes := 0
	for _, l := range lanes {
		facts := len(l) > 0 && factEngines[l[0]]
		if !facts {
			webLanes++
		}
		query := base
		query.Engines = l
		go func() {
			res, err := s.engine.Search(ctx, query)
			answers <- laneAnswer{l, facts, res, err}
		}()
	}
	rules, _ := s.store.ListRules(ctx, uid)
	rules = lensRules(rules, lens)

	var shown, later []*engine.Response
	var errs []error
	var grace <-chan time.Time
	webDone, got := 0, 0
	early := false
	showNow := func() {
		early = true
		if len(errs) == len(lanes) {
			job.showEarly(nil, errs[0])
			return
		}
		out := s.pageOut(ctx, uid, q, cat, tr, page, merge(shown), nil, rules, lens)
		out.MS = time.Since(start).Milliseconds()
		out.Pending = got < len(lanes)
		job.showEarly(out, nil)
	}
	for got < len(lanes) {
		select {
		case a := <-answers:
			got++
			if !a.facts {
				webDone++
			}
			if a.err != nil {
				errs = append(errs, a.err)
				// a lane that failed is an engine not answering this time
				a.res = &engine.Response{Unresponsive: []engine.Unresponsive{{Engine: strings.Join(a.names, ", "), Reason: "no answer"}}}
			}
			if early {
				later = append(later, a.res)
				continue
			}
			shown = append(shown, a.res)
			if grace == nil && len(a.res.Results) > 0 && !a.facts {
				grace = time.After(laneGrace)
			}
			if grace != nil && webDone == webLanes {
				showNow()
			}
		case <-grace:
			if !early {
				showNow()
			}
		case <-ctx.Done():
			got = len(lanes)
		}
	}
	if !early {
		showNow()
	}
	if job.err != nil || job.first == nil {
		return
	}
	final := job.first
	if len(later) > 0 {
		final = s.pageOut(ctx, uid, q, cat, tr, page, merge(shown), merge(later), rules, lens)
		final.MS = time.Since(start).Milliseconds()
	}
	f := *final
	f.Pending = false
	s.remember(key, &f)
	job.final = &f
}

// pageOut is a page as the person sees it: what came first in the engines'
// order with the person's site rules applied, then (if any) what came later
// and was not there yet, ordered the same way below it.
func (s *Server) pageOut(ctx context.Context, uid int64, q, cat, tr string, page int, first, later *engine.Response, rules []store.SiteRule, lens *store.Lens) *searchOut {
	all := first
	if later != nil {
		seen := map[string]bool{}
		for _, r := range first.Results {
			seen[urlKey(r.URL)] = true
		}
		rest := later.Results[:0:0]
		for _, r := range later.Results {
			if !seen[urlKey(r.URL)] {
				rest = append(rest, r)
			}
		}
		later.Results = rest
		all = merge([]*engine.Response{first, later})
		all.Results = append(append([]engine.Result(nil), first.Results...), rest...)
	}
	hits, hidden, hiddenSites, ruleHits := applyRules(first.Results, rules)
	if later != nil {
		more, h2, s2, rh2 := applyRules(later.Results, rules)
		hits = append(hits, more...)
		hidden += h2
		for _, d := range s2 {
			if !has(hiddenSites, d) {
				hiddenSites = append(hiddenSites, d)
			}
		}
		for id, n := range rh2 {
			ruleHits[id] += n
		}
	}
	hits = onlySites(hits, lens)
	if len(ruleHits) > 0 {
		s.store.CountRuleHits(ctx, uid, ruleHits)
	}
	out := &searchOut{Query: q, Category: cat, Page: page, Time: tr, Results: hits, Answers: all.Answers, Infoboxes: all.Infoboxes,
		Suggestions: all.Suggestions, Corrections: all.Corrections, Unresponsive: all.Unresponsive, Hidden: hidden, HiddenSites: hiddenSites,
		More: len(all.Results) >= 8 && page < 20, Engine: s.engineName()}
	if page > 1 {
		out.Infoboxes = []engine.Infobox{}
	}
	return out
}

// urlKey is an address as two engines might both write it.
func urlKey(raw string) string {
	k := strings.ToLower(raw)
	k = strings.TrimPrefix(strings.TrimPrefix(k, "https://"), "http://")
	k = strings.TrimPrefix(k, "www.")
	return strings.TrimRight(k, "/")
}

// merge puts several engines' answers together the way SearXNG would: a
// result more engines found, and found higher up, comes first.
func merge(rs []*engine.Response) *engine.Response {
	out := &engine.Response{Results: []engine.Result{}, Answers: []engine.Answer{}, Infoboxes: []engine.Infobox{}, Suggestions: []string{}, Corrections: []string{}, Unresponsive: []engine.Unresponsive{}}
	type scored struct {
		r     engine.Result
		score float64
		n     int
	}
	byKey := map[string]*scored{}
	var list []*scored
	seenAnswer, seenBox, seenSug, seenCor, seenOff := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, r := range rs {
		for i, x := range r.Results {
			k := urlKey(x.URL)
			if sc, ok := byKey[k]; ok {
				sc.score += 1 / float64(i+1)
				sc.n++
				for _, e := range x.Engines {
					if !has(sc.r.Engines, e) {
						sc.r.Engines = append(sc.r.Engines, e)
					}
				}
				if len(x.Content) > len(sc.r.Content) {
					sc.r.Content = x.Content
				}
				continue
			}
			x.Engines = append([]string(nil), x.Engines...)
			sc := &scored{r: x, score: 1 / float64(i+1), n: 1}
			byKey[k] = sc
			list = append(list, sc)
		}
		for _, a := range r.Answers {
			if !seenAnswer[a.Text] {
				seenAnswer[a.Text] = true
				out.Answers = append(out.Answers, a)
			}
		}
		for _, b := range r.Infoboxes {
			if !seenBox[b.Title+"\x00"+b.URL] {
				seenBox[b.Title+"\x00"+b.URL] = true
				out.Infoboxes = append(out.Infoboxes, b)
			}
		}
		for _, x := range r.Suggestions {
			if !seenSug[x] {
				seenSug[x] = true
				out.Suggestions = append(out.Suggestions, x)
			}
		}
		for _, x := range r.Corrections {
			if !seenCor[x] {
				seenCor[x] = true
				out.Corrections = append(out.Corrections, x)
			}
		}
		for _, x := range r.Unresponsive {
			if !seenOff[x.Engine] {
				seenOff[x.Engine] = true
				out.Unresponsive = append(out.Unresponsive, x)
			}
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].score*float64(list[i].n) > list[j].score*float64(list[j].n)
	})
	for _, sc := range list {
		out.Results = append(out.Results, sc.r)
	}
	return out
}
