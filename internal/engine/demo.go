package engine

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"
)

// Demo answers every search with made-up but believable results, so the app
// can be tried, tested and photographed without a SearXNG (DEMO_DATA=true
// and no SEARXNG_URL). Pictures point at /demo/img/<n>.svg, drawn by the app.
type Demo struct{}

// Configured: the demo engine always answers.
func (Demo) Configured() bool { return true }

var demoSites = []struct{ host, kind string }{
	{"en.wikipedia.org", "wiki"}, {"www.reddit.com", "forum"}, {"github.com", "code"}, {"stackoverflow.com", "qa"},
	{"www.theguardian.com", "news"}, {"www.bbc.com", "news"}, {"news.ycombinator.com", "forum"}, {"developer.mozilla.org", "docs"},
	{"www.nytimes.com", "news"}, {"medium.com", "blog"}, {"www.pinterest.com", "pins"}, {"arstechnica.com", "news"},
	{"www.seriouseats.com", "recipe"}, {"www.allrecipes.com", "recipe"}, {"www.lonelyplanet.com", "travel"}, {"www.youtube.com", "video"},
	{"go.dev", "docs"}, {"www.quora.com", "qa"}, {"www.tripadvisor.com", "travel"}, {"www.britannica.com", "wiki"},
}

var demoEngines = []string{"google", "brave", "duckduckgo", "bing", "startpage", "qwant", "mojeek", "wikipedia"}

func seed(s string) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(s)))
	return rand.New(rand.NewSource(int64(h.Sum64() & 0x7fffffffffffffff)))
}

func title(q string) string {
	w := strings.Fields(q)
	for i, s := range w {
		if len(s) > 0 {
			w[i] = strings.ToUpper(s[:1]) + s[1:]
		}
	}
	return strings.Join(w, " ")
}

// Search makes up a page of results for q.
func (Demo) Search(_ context.Context, q Query) (*Response, error) {
	r := seed(q.Q + "|" + q.Category + fmt.Sprint(q.Page))
	T := title(q.Q)
	slug := strings.ReplaceAll(strings.ToLower(strings.Join(strings.Fields(q.Q), "-")), "/", "")
	out := &Response{Results: []Result{}, Answers: []Answer{}, Infoboxes: []Infobox{}, Suggestions: []string{}, Corrections: []string{}, Unresponsive: []Unresponsive{}}
	pick := func(n int) []string {
		p := r.Perm(len(demoEngines))[:n]
		var e []string
		for _, i := range p {
			e = append(e, demoEngines[i])
		}
		return e
	}
	switch q.Category {
	case "images":
		for i := 0; i < 30; i++ {
			s := demoSites[r.Intn(len(demoSites))]
			n := r.Intn(24)
			out.Results = append(out.Results, Result{URL: fmt.Sprintf("https://%s/%s/%d", s.host, slug, i), Title: fmt.Sprintf("%s — photo %d", T, i+1),
				Engine: "bing images", Engines: []string{"bing images"}, Category: "images", Score: 1,
				Thumbnail: fmt.Sprintf("/demo/img/%d.svg", n), Image: fmt.Sprintf("/demo/img/%d.svg", n), Resolution: fmt.Sprintf("%d × %d", 800+r.Intn(8)*160, 600+r.Intn(6)*120), Source: s.host})
		}
		return out, nil
	case "videos":
		for i := 0; i < 14; i++ {
			out.Results = append(out.Results, Result{URL: fmt.Sprintf("https://www.youtube.com/watch?v=demo%s%d", slug, i), Title: []string{"How it really works: ", "A beginner's guide to ", "Everything about ", "10 things about ", "Inside "}[i%5] + T,
				Content: "A clear, calm walk through " + q.Q + " with examples, history and the parts most people get wrong.", Engine: "youtube", Engines: pick(1 + r.Intn(2)), Category: "videos",
				Thumbnail: fmt.Sprintf("/demo/img/%d.svg", r.Intn(24)), Length: fmt.Sprintf("%d:%02d", 3+r.Intn(40), r.Intn(60)), Author: []string{"Veritasium", "Kurzgesagt", "Tom Scott", "Real Engineering", "Vox"}[r.Intn(5)], Score: float64(14 - i)})
		}
		return out, nil
	case "news":
		for i := 0; i < 12; i++ {
			s := []string{"www.theguardian.com", "www.bbc.com", "arstechnica.com", "www.nytimes.com", "apnews.com"}[r.Intn(5)]
			out.Results = append(out.Results, Result{URL: fmt.Sprintf("https://%s/2026/10/%s-%d", s, slug, i), Title: []string{"What's next for ", "Why everyone is talking about ", "The quiet return of ", "A closer look at ", "New study changes how we think about "}[i%5] + q.Q,
				Content: "Reporting on " + q.Q + ": what changed this week, who it affects and what experts expect over the coming months.", Engine: "bing news", Engines: pick(1 + r.Intn(2)), Category: "news",
				Metadata: fmt.Sprintf("%d days ago | %s", 1+i, strings.TrimPrefix(s, "www.")), Thumbnail: fmt.Sprintf("/demo/img/%d.svg", r.Intn(24)), Score: float64(12 - i)})
		}
		return out, nil
	}
	snips := []string{
		"%s explained: the background, how it works today and the details that usually get skipped. Updated with sources and examples.",
		"A practical guide to %s — what to look for, common mistakes and a step-by-step walkthrough you can follow in an afternoon.",
		"Discussion: people who have tried %s share what worked, what did not, and what they would do differently next time.",
		"Everything you need to know about %s, from the basics to the advanced bits, with pictures and a short summary at the top.",
		"The official documentation for %s: reference, tutorials and frequently asked questions.",
	}
	for i := 0; i < 10; i++ {
		s := demoSites[r.Intn(len(demoSites))]
		if i == 0 {
			s = demoSites[0]
		}
		u := fmt.Sprintf("https://%s/%s", s.host, slug)
		if i > 0 {
			u = fmt.Sprintf("https://%s/%s/%d", s.host, slug, i)
		}
		t := T
		switch s.kind {
		case "wiki":
			t = T + " - " + map[bool]string{true: "Wikipedia", false: "Britannica"}[s.host == "en.wikipedia.org"]
		case "forum":
			t = "Anyone tried " + q.Q + "? : r/" + strings.ReplaceAll(strings.ToLower(T), " ", "")
		case "qa":
			t = "What is the best way to approach " + q.Q + "?"
		case "code":
			t = "awesome-" + slug + ": a curated list of " + q.Q + " resources"
		case "recipe":
			t = "The Best " + T + " Recipe"
		case "travel":
			t = T + ": the complete guide (2026)"
		}
		out.Results = append(out.Results, Result{URL: u, Title: t, Content: fmt.Sprintf(snips[(i+r.Intn(5))%5], q.Q), Engine: demoEngines[i%len(demoEngines)],
			Engines: pick(1 + r.Intn(4)), Score: float64(20-i) / 4, Category: "general"})
	}
	if q.Page <= 1 {
		out.Infoboxes = append(out.Infoboxes, Infobox{Title: T, URL: "https://en.wikipedia.org/wiki/" + strings.ReplaceAll(T, " ", "_"), Image: fmt.Sprintf("/demo/img/%d.svg", r.Intn(24)),
			Content: T + " is the subject of this search. This summary comes from Wikipedia and Wikidata: a few sentences about what it is, where it comes from and why it matters.",
			Links:   []Link{{Title: "Wikipedia", URL: "https://en.wikipedia.org/wiki/" + strings.ReplaceAll(T, " ", "_")}, {Title: "Official website", URL: "https://" + slug + ".example.org", Official: true}},
			Attrs:   []Attr{{Label: "Known for", Value: "Being searched for"}, {Label: "First described", Value: fmt.Sprint(1850 + r.Intn(170))}}})
		out.Suggestions = append(out.Suggestions, q.Q+" guide", q.Q+" for beginners", "best "+q.Q, q.Q+" vs alternatives")
		out.Unresponsive = append(out.Unresponsive, Unresponsive{Engine: "duckduckgo", Reason: "CAPTCHA"})
	}
	return out, nil
}

// Suggest completes what is typed.
func (Demo) Suggest(_ context.Context, q, _ string) ([]string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []string{}, nil
	}
	return []string{q + " recipe", q + " near me", q + " meaning", q + " 2026", "how to " + q}, nil
}

// Engines is a short, realistic list.
func (Demo) Engines(context.Context) ([]Info, error) {
	return []Info{
		{Name: "bing", Shortcut: "bi", Categories: []string{"general", "web"}, Enabled: false, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "bing images", Shortcut: "bii", Categories: []string{"images", "web"}, Enabled: true, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "bing news", Shortcut: "bin", Categories: []string{"news", "web"}, Enabled: true, Paging: true, TimeRange: true},
		{Name: "bing videos", Shortcut: "biv", Categories: []string{"videos", "web"}, Enabled: true, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "brave", Shortcut: "br", Categories: []string{"general", "web"}, Enabled: true, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "duckduckgo", Shortcut: "ddg", Categories: []string{"general", "web"}, Enabled: true, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "duckduckgo images", Shortcut: "ddi", Categories: []string{"images", "web"}, Enabled: false, Paging: true, SafeSearch: true},
		{Name: "duckduckgo news", Shortcut: "ddn", Categories: []string{"news", "web"}, Enabled: true, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "google", Shortcut: "go", Categories: []string{"general", "web"}, Enabled: true, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "google images", Shortcut: "goi", Categories: []string{"images", "web"}, Enabled: true, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "google news", Shortcut: "gon", Categories: []string{"news"}, Enabled: true, SafeSearch: true},
		{Name: "mojeek", Shortcut: "mjk", Categories: []string{"general", "web"}, Enabled: false, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "qwant", Shortcut: "qw", Categories: []string{"general", "web"}, Enabled: true, Paging: true, SafeSearch: true},
		{Name: "startpage", Shortcut: "sp", Categories: []string{"general", "web"}, Enabled: true, Paging: true, TimeRange: true, SafeSearch: true},
		{Name: "vimeo", Shortcut: "vm", Categories: []string{"videos"}, Enabled: true, Paging: true},
		{Name: "wikipedia", Shortcut: "wp", Categories: []string{"general"}, Enabled: true},
		{Name: "youtube", Shortcut: "yt", Categories: []string{"videos", "music"}, Enabled: true, Paging: true, TimeRange: true},
	}, nil
}
