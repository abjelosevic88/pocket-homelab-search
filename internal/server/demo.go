package server

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-search/internal/read"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// ---- demo data (DEMO_DATA=true): months of believable history, rules and
// bangs for the owner, made-up results (engine.Demo), pictures drawn here ----

var demoQueries = []string{
	"sourdough starter not rising", "best hiking trails durmitor", "how to descale espresso machine", "golang sqlite wal mode",
	"weather sarajevo weekend", "flight prices sarajevo vienna", "kids birthday party ideas", "audi a4 b9 service interval",
	"tailscale exit node setup", "nikon z6 vs sony a7 iii", "vegetarian lasagna recipe", "how many calories in burek",
	"the bear season 4 release", "home assistant zigbee dongle", "plitvice lakes opening hours", "jellyfin hardware transcoding intel",
	"best budget mechanical keyboard", "how to grow tomatoes on a balcony", "postgres vs sqlite for small apps", "learn german a2 grammar",
	"winter tyres 225/45 r17", "running plan 10k beginner", "how to fix squeaky door", "bosnian coffee vs turkish coffee",
}

var demoOpened = []struct{ title, url string }{
	{"Sourdough starter troubleshooting — The Perfect Loaf", "https://www.theperfectloaf.com/sourdough-starter-troubleshooting/"},
	{"Durmitor National Park - Wikipedia", "https://en.wikipedia.org/wiki/Durmitor_National_Park"},
	{"SQLite Write-Ahead Logging", "https://www.sqlite.org/wal.html"},
	{"Exit nodes · Tailscale Docs", "https://tailscale.com/kb/1103/exit-nodes"},
	{"The Best Vegetarian Lasagna - Serious Eats", "https://www.seriouseats.com/vegetarian-lasagna"},
	{"Plitvice Lakes National Park - opening hours", "https://np-plitvicka-jezera.hr/en/"},
}

func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	if n, _ := s.store.CountsFor(ctx, uid)["history"]; n > 0 {
		return nil
	}
	r := rand.New(rand.NewSource(42))
	now := time.Now().UTC()
	// ~4 months of searches, more on weekday evenings
	for d := 120; d >= 0; d-- {
		day := now.AddDate(0, 0, -d)
		for k := 0; k < r.Intn(5); k++ {
			at := time.Date(day.Year(), day.Month(), day.Day(), 7+r.Intn(15), r.Intn(60), r.Intn(60), 0, time.UTC)
			if at.After(now) {
				continue
			}
			q := demoQueries[r.Intn(len(demoQueries))]
			cat := []string{"general", "general", "general", "images", "news", "videos"}[r.Intn(6)]
			if _, err := s.store.DB.ExecContext(ctx, `INSERT INTO history (user_id, kind, query, category, results, created_at) VALUES (?, 'search', ?, ?, ?, ?)`,
				uid, q, cat, 8+r.Intn(30), at.Format(time.RFC3339Nano)); err != nil {
				return err
			}
			if r.Intn(3) == 0 {
				o := demoOpened[r.Intn(len(demoOpened))]
				if _, err := s.store.DB.ExecContext(ctx, `INSERT INTO history (user_id, kind, query, url, title, created_at) VALUES (?, 'open', ?, ?, ?, ?)`,
					uid, q, o.url, o.title, at.Add(40*time.Second).Format(time.RFC3339Nano)); err != nil {
					return err
				}
			}
		}
	}
	for _, x := range []store.SiteRule{
		{Domain: "en.wikipedia.org", Action: store.RulePin, Note: "Always first"},
		{Domain: "developer.mozilla.org", Action: store.RuleBoost},
		{Domain: "github.com", Action: store.RuleBoost},
		{Domain: "stackoverflow.com", Action: store.RuleBoost},
		{Domain: "medium.com", Action: store.RuleLower, Note: "Often behind a paywall"},
		{Domain: "www.quora.com", Action: store.RuleLower},
		{Domain: "pinterest.com", Action: store.RuleBlock, Note: "Pictures without the recipe"},
		{Domain: "w3schools.com", Action: store.RuleBlock},
	} {
		x := x
		if err := s.store.SetRule(ctx, uid, &x); err != nil {
			return err
		}
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE site_rules SET hits = ? WHERE id = ?`, 5+r.Intn(200), x.ID)
	}
	for _, b := range []store.Bang{
		{Trigger: "jf", Name: "Jellyfin", URL: "https://watch.example.com/search?q={q}"},
		{Trigger: "ah", Name: "Arch packages", URL: "https://archlinux.org/packages/?q={q}"},
		{Trigger: "mdb", Name: "MusicBrainz artists", URL: "https://musicbrainz.org/search?type=artist&query={q}"},
	} {
		b := b
		if err := s.store.CreateBang(ctx, uid, &b); err != nil {
			return err
		}
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE bangs SET uses = ? WHERE id = ?`, r.Intn(40), b.ID)
	}
	for _, v := range []store.Save{
		{URL: "https://en.wikipedia.org/sourdough-starter-not-rising", App: "reader", Title: "Sourdough starter not rising - Wikipedia"},
		{URL: "https://www.seriouseats.com/vegetarian-lasagna-recipe", App: "notes", Title: "The Best Vegetarian Lasagna Recipe"},
		{URL: "https://www.tripadvisor.com/plitvice-lakes-opening-hours", App: "tasks", Title: "Plitvice Lakes opening hours"},
	} {
		v := v
		if err := s.store.AddSave(ctx, uid, &v); err != nil {
			return err
		}
	}
	return nil
}

func hue(s string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32() % 360)
}

// handleDemoImage draws picture n: a landscape in calm colours.
func (s *Server) handleDemoImage(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(strings.TrimSuffix(r.PathValue("n"), ".svg"))
	if n == 0 {
		n, _ = strconv.Atoi(strings.TrimSuffix(lastSegment(r.URL.Path), ".svg"))
	}
	rr := rand.New(rand.NewSource(int64(n) + 7))
	h := (n*47 + 190) % 360
	sun := 20 + rr.Intn(60)
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 300" width="400" height="300">
<defs><linearGradient id="s" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="hsl(%d,70%%,78%%)"/><stop offset="1" stop-color="hsl(%d,60%%,92%%)"/></linearGradient></defs>
<rect width="400" height="300" fill="url(#s)"/><circle cx="%d" cy="%d" r="28" fill="hsl(%d,90%%,70%%)" opacity=".9"/>
<path d="M0 210 L%d 120 L%d 200 L%d 140 L400 215 L400 300 L0 300Z" fill="hsl(%d,30%%,45%%)"/>
<path d="M0 250 Q100 215 200 245 T400 240 L400 300 L0 300Z" fill="hsl(%d,40%%,32%%)"/></svg>`,
		h, (h+30)%360, 60+sun*4, 50+rr.Intn(40), (h+180)%360, 80+rr.Intn(60), 170+rr.Intn(40), 260+rr.Intn(60), (h+20)%360, (h+40)%360)
	imageHeaders(w, "image/svg+xml", 86400)
	_, _ = w.Write([]byte(svg))
}

func lastSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// demoFavicon is a coloured tile with the site's first letter.
func demoFavicon(domain string) string {
	l := strings.ToUpper(domain[:1])
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="7" fill="hsl(%d,55%%,48%%)"/><text x="16" y="22" font-family="system-ui,sans-serif" font-size="17" font-weight="700" fill="#fff" text-anchor="middle">%s</text></svg>`, hue(domain), l)
}

// demoArticle is the reading view of a made-up page.
func demoArticle(raw string, images bool) *read.Article {
	d := domainOf(raw)
	words := strings.Fields(strings.NewReplacer("-", " ", "/", " ", "_", " ").Replace(lastSegment(strings.TrimRight(raw, "/0123456789"))))
	t := strings.Join(words, " ")
	if t == "" {
		t = d
	}
	T := strings.ToUpper(t[:1]) + t[1:]
	img := ""
	if images {
		img = `<figure><img src="/demo/img/7.svg" alt="" loading="lazy"><figcaption>A picture from the page, loaded through your server.</figcaption></figure>`
	}
	body := fmt.Sprintf(`<p><b>%s</b> comes up again and again, so here is the short version first: it is simpler than it looks, and most problems come from three small mistakes.</p>
%s<h2>Where to start</h2><p>Begin with the basics and change one thing at a time. Write down what you did, so that when something works you know why. Most people skip this step and end up guessing.</p>
<h2>The three common mistakes</h2><ul><li>Doing everything at once instead of one step at a time.</li><li>Trusting the first answer found instead of comparing two or three sources.</li><li>Giving up a day too early — many things simply need time.</li></ul>
<h2>What the experts say</h2><p>Opinions differ in the details, but everyone agrees on the fundamentals: patience, a little measuring, and paying attention to what actually happens rather than what a guide promises.</p>
<blockquote>“Read the whole page once before you start. It saves an hour later.”</blockquote>
<p>This page was opened in Pocket Search's reading view: your server fetched it, left out the scripts, trackers and adverts, and sent you only the text and pictures. The site saw the server, not you.</p>`, T, img)
	text := read.PlainText(body)
	return &read.Article{URL: raw, Title: T + " — the complete guide", Byline: "By Jordan Lee", Site: d, Excerpt: "Everything you need to know about " + t + ".",
		Content: body, Text: text, Words: len(strings.Fields(text)), Minutes: 2, Published: time.Now().AddDate(0, 0, -12).UTC().Format(time.RFC3339)}
}

// demoAnswer stands in for the AI when the demo has none.
func demoAnswer(q string, sources []source) *answerOut {
	return &answerOut{Answer: fmt.Sprintf("**%s** is mostly about getting the basics right and changing one thing at a time [1]. People who tried it say patience matters more than equipment [3], and the official guide recommends checking the details before you start [2].\n\n- Start simple and write down what you change [1]\n- Compare two or three sources before trusting one [4]", strings.ToUpper(q[:1])+q[1:]),
		Sources: sources, Provider: "demo"}
}

// demoSections are made-up matches from a few Pocket apps.
func demoSections(q, only string, limit int) []appSection {
	T := strings.ToUpper(q[:1]) + q[1:]
	all := []appSection{
		{App: "notes", Name: "Pocket Notes", Items: []appItem{
			{Title: T + " — what worked", Snippet: "Notes from last time: start small, write down every change, compare two sources before buying anything.", Date: time.Now().AddDate(0, 0, -9).Format(time.RFC3339), URL: "#", Kind: "note"},
			{Title: "Ideas: " + q, Snippet: "Links and thoughts collected over a few weeks.", Date: time.Now().AddDate(0, -2, 0).Format(time.RFC3339), URL: "#", Kind: "note"}}},
		{App: "reader", Name: "Pocket Reader", Items: []appItem{
			{Title: "The long read: " + q, Snippet: "Saved from The Guardian · 14 min read", Date: time.Now().AddDate(0, 0, -21).Format(time.RFC3339), URL: "#", Kind: "article"}}},
		{App: "tasks", Name: "Pocket Tasks", Items: []appItem{
			{Title: "Look into " + q, Snippet: "Due Friday · Home", URL: "#", Kind: "task"}}},
		{App: "documents", Name: "Pocket Documents", Items: []appItem{}},
		{App: "bookshelf", Name: "Pocket Bookshelf", Items: []appItem{}},
	}
	out := []appSection{}
	for _, s := range all {
		if only != "" && s.App != only {
			continue
		}
		if len(s.Items) > limit {
			s.Items = s.Items[:limit]
		}
		s.More, s.MS = "#", int64(40+len(s.App)*9)
		out = append(out, s)
	}
	return out
}
