package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// ---- lenses: a saved way of searching, one click under the search box ----

// defaultLenses are what a person starts with (once; they can delete them).
var defaultLenses = []store.Lens{
	{Name: "Recipes", Icon: "🍲", Words: "recipe",
		Boost: "seriouseats.com\nbbcgoodfood.com\nkingarthurbaking.com\nbudgetbytes.com\nsimplyrecipes.com\nnytimes.com\nrecipetineats.com\ncoolinarika.com",
		Hide:  "pinterest.com\nyummly.com"},
	{Name: "Code", Icon: "💻",
		Boost: "github.com\nstackoverflow.com\ndeveloper.mozilla.org\ngo.dev\npkg.go.dev\ndocs.python.org\nrust-lang.org\nwiki.archlinux.org\nnews.ycombinator.com\nsuperuser.com\nserverfault.com\nunix.stackexchange.com",
		Hide:  "w3schools.com\ngeeksforgeeks.org\ntutorialspoint.com"},
	{Name: "Forums", Icon: "💬", Sites: "reddit.com\nnews.ycombinator.com\nstackexchange.com\nstackoverflow.com\nlobste.rs\ndiscourse.org\nforum.klix.ba"},
	{Name: "Local", Icon: "📍", Sites: "ba\nrs\nhr\nme", Language: "bs"},
	{Name: "Science", Icon: "🔬",
		Boost: "wikipedia.org\nbritannica.com\nnature.com\nscience.org\nsciencedirect.com\narxiv.org\nnih.gov\nnasa.gov\nscientificamerican.com\nquantamagazine.org"},
}

// ensureLenses gives a person the default lenses the first time they look.
func (s *Server) ensureLenses(ctx context.Context, uid int64) {
	key := fmt.Sprintf("lenses-seeded:%d", uid)
	if _, _, done := s.store.CacheGet(ctx, key); done {
		return
	}
	if have, err := s.store.ListLenses(ctx, uid); err == nil && len(have) == 0 {
		for _, l := range defaultLenses {
			l := l
			_ = s.store.CreateLens(ctx, uid, &l)
		}
	}
	_ = s.store.CachePut(ctx, key, "1")
}

func cleanLens(l *store.Lens) string {
	l.Name = strings.TrimSpace(l.Name)
	l.Icon = strings.TrimSpace(l.Icon)
	l.Words = oneLine(l.Words, 200)
	l.Language = strings.TrimSpace(l.Language)
	join := func(v string) string { return strings.Join(store.Domains(v), "\n") }
	l.Sites, l.Boost, l.Hide = join(l.Sites), join(l.Boost), join(l.Hide)
	if l.Name == "" || len([]rune(l.Name)) > 40 {
		return "A lens needs a short name (40 characters at most)"
	}
	if len([]rune(l.Icon)) > 4 {
		return "The icon is one emoji"
	}
	if len(l.Language) > 12 || strings.ContainsAny(l.Language, " /?&") {
		return "unknown language"
	}
	if l.Sites == "" && l.Boost == "" && l.Hide == "" && l.Words == "" && l.Language == "" {
		return "A lens needs at least one site, word or language"
	}
	return ""
}

func (s *Server) handleListLenses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	s.ensureLenses(ctx, uid)
	list, err := s.store.ListLenses(ctx, uid)
	if err != nil {
		s.fail(w, err, "lenses")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateLens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in store.Lens
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := cleanLens(&in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	s.ensureLenses(ctx, actorID(ctx))
	if err := s.store.CreateLens(ctx, actorID(ctx), &in); err != nil {
		s.fail(w, err, "lens")
		return
	}
	writeJSON(w, http.StatusCreated, in)
}

func (s *Server) handleUpdateLens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	cur, err := s.store.GetLens(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "lens")
		return
	}
	if _, err := decodeKeys(r, cur); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	cur.ID = idParam(r, "id")
	if msg := cleanLens(cur); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateLens(ctx, uid, cur); err != nil {
		s.fail(w, err, "lens")
		return
	}
	s.forgetResults(uid)
	out, _ := s.store.GetLens(ctx, uid, cur.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteLens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteLens(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "lens")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// lensQuery is what the engines are asked through a lens: the words added and,
// for "only these sites", a site: filter the big engines understand (the
// results are filtered again afterwards, for the engines that ignore it).
func lensQuery(q string, l *store.Lens) string {
	if l == nil {
		return q
	}
	if l.Words != "" && !strings.Contains(strings.ToLower(q), strings.ToLower(l.Words)) {
		q += " " + l.Words
	}
	if sites := store.Domains(l.Sites); len(sites) > 0 {
		parts := make([]string, 0, len(sites))
		for _, d := range sites[:min(len(sites), 8)] {
			parts = append(parts, "site:"+d)
		}
		q += " (" + strings.Join(parts, " OR ") + ")"
	}
	return q
}

// lensRules adds a lens's raised and hidden sites to a person's own rules
// (theirs win for a site both name).
func lensRules(rules []store.SiteRule, l *store.Lens) []store.SiteRule {
	if l == nil {
		return rules
	}
	out := append([]store.SiteRule(nil), rules...)
	have := map[string]bool{}
	for _, r := range rules {
		have[r.Domain] = true
	}
	for _, d := range store.Domains(l.Boost) {
		if !have[d] {
			out = append(out, store.SiteRule{ID: -1, Domain: d, Action: store.RuleBoost})
		}
	}
	for _, d := range store.Domains(l.Hide) {
		if !have[d] {
			out = append(out, store.SiteRule{ID: -1, Domain: d, Action: store.RuleBlock})
		}
	}
	return out
}

// onlySites keeps the results from a lens's sites.
func onlySites(hits []hit, l *store.Lens) []hit {
	if l == nil {
		return hits
	}
	sites := store.Domains(l.Sites)
	if len(sites) == 0 {
		return hits
	}
	out := hits[:0]
	for _, h := range hits {
		for _, d := range sites {
			if h.Domain == d || strings.HasSuffix(h.Domain, "."+d) {
				out = append(out, h)
				break
			}
		}
	}
	return out
}
