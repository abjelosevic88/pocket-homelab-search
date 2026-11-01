package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-search/internal/read"
)

// ---- AI answers: a short answer above the results, with its sources ----
//
// Built from the results the person is looking at (their rules applied) and,
// when "read the top pages" is on, the readable text of the first three —
// fetched by the server like the reading view. Every claim points at a
// numbered source; when the results do not say, the answer says so.

type source struct {
	N     int    `json:"n"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Read  bool   `json:"read"` // its page text was read, not only the snippet
}

type answerOut struct {
	Answer   string   `json:"answer"`
	Sources  []source `json:"sources"`
	Provider string   `json:"provider"`
	MS       int64    `json:"ms"`
}

const answerSystem = `You answer a person's web search using ONLY the numbered search results given.
- Answer in the language of the search. Start with the answer itself, no preamble ("Based on the results…").
- At most 120 words; short paragraphs or a few bullet points ("- ") when listing. Plain text, **bold** allowed, no headings.
- After every claim cite its source(s) like [1] or [2][4]. Never cite a number that is not given.
- When the results disagree, say so briefly. When they do not answer the question, say "The results don't say." and stop.
- Never invent facts, prices, dates or links.`

// handleAnswer: POST /api/v1/answer {"q": "...", "cat": "general"}
func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Q    string `json:"q"`
		Cat  string `json:"cat"`
		Time string `json:"time"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Q) == "" {
		writeErr(w, http.StatusBadRequest, "q is required")
		return
	}
	if in.Cat == "" || in.Cat == "apps" {
		in.Cat = "general"
	}
	if _, ok := categories[in.Cat]; !ok || !timeRanges[in.Time] {
		writeErr(w, http.StatusBadRequest, "unknown category or time")
		return
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	if st.AIAnswers == "off" {
		writeErr(w, http.StatusConflict, "AI answers are switched off (Settings → AI)")
		return
	}
	cl, provider, why := s.aiFor(ctx, u.ID, st)
	demo := s.cfg.DemoData && s.engineName() == "demo"
	if cl == nil && !demo {
		writeErr(w, http.StatusServiceUnavailable, "No AI is set up: "+why)
		return
	}
	start := time.Now()
	res, err := s.search(ctx, u, st, strings.TrimSpace(in.Q), in.Cat, in.Time, "", 1, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	n := min(len(res.Results), 8)
	if n == 0 {
		writeJSON(w, http.StatusOK, answerOut{Answer: "The results don't say.", Sources: []source{}, Provider: provider})
		return
	}
	sources := make([]source, n)
	texts := make([]string, n)
	for i := 0; i < n; i++ {
		h := res.Results[i]
		sources[i] = source{N: i + 1, Title: h.Title, URL: h.URL}
		texts[i] = h.Content
	}
	if cl == nil {
		writeJSON(w, http.StatusOK, demoAnswer(in.Q, sources))
		return
	}
	if st.AIReadPages && !demo {
		var wg sync.WaitGroup
		for i := 0; i < min(n, 3); i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				rctx, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				if a, err := s.fetch.Extract(rctx, sources[i].URL, nil); err == nil && a.Text != "" {
					texts[i] = read.Excerpt(strings.Join(strings.Fields(a.Text), " "), 3500)
					sources[i].Read = true
				}
			}(i)
		}
		wg.Wait()
	}
	var p strings.Builder
	fmt.Fprintf(&p, "Search: %s\nToday: %s\n\nResults:\n", in.Q, time.Now().Format("2 January 2006"))
	for i, src := range sources {
		fmt.Fprintf(&p, "[%d] %s\n%s\n%s\n\n", src.N, src.Title, src.URL, oneLine(texts[i], 3600))
	}
	actx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	txt, err := cl.Complete(actx, answerSystem, p.String(), false)
	if err != nil {
		s.log.Warn("ai answer failed", "err", err)
		writeErr(w, http.StatusBadGateway, aiTrouble(err))
		return
	}
	writeJSON(w, http.StatusOK, answerOut{Answer: strings.TrimSpace(txt), Sources: sources, Provider: provider, MS: time.Since(start).Milliseconds()})
}

// aiTrouble says in plain words why there is no answer; the technical detail
// stays in the log.
func aiTrouble(err error) string {
	e := strings.ToLower(err.Error())
	switch {
	case strings.Contains(e, "connection refused"), strings.Contains(e, "no such host"), strings.Contains(e, "eof"):
		return "Pocket AI can't be reached right now (it may be restarting). Try again in a moment."
	case strings.Contains(e, "deadline exceeded"), strings.Contains(e, "timeout"):
		return "The AI took too long to answer. Try again."
	case strings.Contains(e, "401"), strings.Contains(e, "403"), strings.Contains(e, "unauthorized"):
		return "The AI refused the request: check Settings → AI (or the Pocket AI link in Pocket Core)."
	case strings.Contains(e, "429"):
		return "The AI is busy (too many requests). Try again in a minute."
	}
	return "The AI did not answer: " + err.Error()
}
