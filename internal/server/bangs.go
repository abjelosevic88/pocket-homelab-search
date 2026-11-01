package server

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// ---- bangs: "!w eiffel tower" jumps straight to Wikipedia ----
//
// A bang is a word starting with "!" at the start or the end of a search.
// A person's own bangs win over the built-in ones. Bangs whose address
// starts with "/" stay inside Pocket Search (pictures, news, your apps, the
// reading view).

type bang struct {
	Trigger string `json:"trigger"`
	Name    string `json:"name"`
	URL     string `json:"url"`  // {q} = the words
	Home    string `json:"home"` // when there are no words
	Group   string `json:"group"`
	Own     bool   `json:"own,omitempty"`
	ID      int64  `json:"id,omitempty"`
}

var builtinBangs = []bang{
	// in Pocket Search
	{"i", "Pictures", "/search?cat=images&q={q}", "/", "Pocket Search", false, 0},
	{"v", "Videos", "/search?cat=videos&q={q}", "/", "Pocket Search", false, 0},
	{"n", "News", "/search?cat=news&q={q}", "/", "Pocket Search", false, 0},
	{"apps", "Your Pocket apps", "/search?cat=apps&q={q}", "/", "Pocket Search", false, 0},
	{"ai", "Search with an AI answer", "/search?ai=1&q={q}", "/", "Pocket Search", false, 0},
	{"read", "Open a page in the reading view", "/read?url={q}", "/", "Pocket Search", false, 0},
	// search engines
	{"g", "Google", "https://www.google.com/search?q={q}", "https://www.google.com/", "Search engines", false, 0},
	{"ddg", "DuckDuckGo", "https://duckduckgo.com/?q={q}", "https://duckduckgo.com/", "Search engines", false, 0},
	{"b", "Bing", "https://www.bing.com/search?q={q}", "https://www.bing.com/", "Search engines", false, 0},
	{"br", "Brave Search", "https://search.brave.com/search?q={q}", "https://search.brave.com/", "Search engines", false, 0},
	{"sp", "Startpage", "https://www.startpage.com/do/search?q={q}", "https://www.startpage.com/", "Search engines", false, 0},
	{"gi", "Google Images", "https://www.google.com/search?tbm=isch&q={q}", "https://images.google.com/", "Search engines", false, 0},
	{"scholar", "Google Scholar", "https://scholar.google.com/scholar?q={q}", "https://scholar.google.com/", "Search engines", false, 0},
	{"wa", "Wolfram|Alpha", "https://www.wolframalpha.com/input?i={q}", "https://www.wolframalpha.com/", "Search engines", false, 0},
	// knowledge
	{"w", "Wikipedia", "https://en.wikipedia.org/wiki/Special:Search?search={q}", "https://en.wikipedia.org/", "Knowledge", false, 0},
	{"wsr", "Wikipedia (Serbian)", "https://sr.wikipedia.org/wiki/Special:Search?search={q}", "https://sr.wikipedia.org/", "Knowledge", false, 0},
	{"wde", "Wikipedia (German)", "https://de.wikipedia.org/wiki/Special:Search?search={q}", "https://de.wikipedia.org/", "Knowledge", false, 0},
	{"wt", "Wiktionary", "https://en.wiktionary.org/wiki/Special:Search?search={q}", "https://en.wiktionary.org/", "Knowledge", false, 0},
	{"arxiv", "arXiv", "https://arxiv.org/a/search?query={q}", "https://arxiv.org/", "Knowledge", false, 0},
	{"wb", "Wayback Machine", "https://web.archive.org/web/*/{q}", "https://web.archive.org/", "Knowledge", false, 0},
	{"ia", "Internet Archive", "https://archive.org/search?query={q}", "https://archive.org/", "Knowledge", false, 0},
	{"gt", "Google Translate", "https://translate.google.com/?sl=auto&tl=en&text={q}", "https://translate.google.com/", "Knowledge", false, 0},
	{"dl", "DeepL", "https://www.deepl.com/translator#auto/en/{q}", "https://www.deepl.com/translator", "Knowledge", false, 0},
	// maps & travel
	{"m", "OpenStreetMap", "https://www.openstreetmap.org/search?query={q}", "https://www.openstreetmap.org/", "Maps & travel", false, 0},
	{"gm", "Google Maps", "https://www.google.com/maps/search/{q}", "https://www.google.com/maps", "Maps & travel", false, 0},
	{"ta", "Tripadvisor", "https://www.tripadvisor.com/Search?q={q}", "https://www.tripadvisor.com/", "Maps & travel", false, 0},
	{"bk", "Booking.com", "https://www.booking.com/searchresults.html?ss={q}", "https://www.booking.com/", "Maps & travel", false, 0},
	// video, music, film, books
	{"yt", "YouTube", "https://www.youtube.com/results?search_query={q}", "https://www.youtube.com/", "Media", false, 0},
	{"imdb", "IMDb", "https://www.imdb.com/find/?q={q}", "https://www.imdb.com/", "Media", false, 0},
	{"tmdb", "The Movie Database", "https://www.themoviedb.org/search?query={q}", "https://www.themoviedb.org/", "Media", false, 0},
	{"lb", "Letterboxd", "https://letterboxd.com/search/{q}/", "https://letterboxd.com/", "Media", false, 0},
	{"genius", "Genius lyrics", "https://genius.com/search?q={q}", "https://genius.com/", "Media", false, 0},
	{"mb", "MusicBrainz", "https://musicbrainz.org/search?type=release_group&query={q}", "https://musicbrainz.org/", "Media", false, 0},
	{"discogs", "Discogs", "https://www.discogs.com/search/?q={q}", "https://www.discogs.com/", "Media", false, 0},
	{"bc", "Bandcamp", "https://bandcamp.com/search?q={q}", "https://bandcamp.com/", "Media", false, 0},
	{"gr", "Goodreads", "https://www.goodreads.com/search?q={q}", "https://www.goodreads.com/", "Media", false, 0},
	// social
	{"r", "Reddit", "https://www.reddit.com/search/?q={q}", "https://www.reddit.com/", "Social", false, 0},
	{"hn", "Hacker News", "https://hn.algolia.com/?q={q}", "https://news.ycombinator.com/", "Social", false, 0},
	{"x", "X (Twitter)", "https://x.com/search?q={q}", "https://x.com/", "Social", false, 0},
	// shopping
	{"a", "Amazon", "https://www.amazon.com/s?k={q}", "https://www.amazon.com/", "Shopping", false, 0},
	{"ade", "Amazon.de", "https://www.amazon.de/s?k={q}", "https://www.amazon.de/", "Shopping", false, 0},
	{"ebay", "eBay", "https://www.ebay.com/sch/i.html?_nkw={q}", "https://www.ebay.com/", "Shopping", false, 0},
	{"ali", "AliExpress", "https://www.aliexpress.com/wholesale?SearchText={q}", "https://www.aliexpress.com/", "Shopping", false, 0},
	{"olx", "OLX.ba", "https://olx.ba/pretraga?q={q}", "https://olx.ba/", "Shopping", false, 0},
	{"kp", "KupujemProdajem", "https://www.kupujemprodajem.com/pretraga?keywords={q}", "https://www.kupujemprodajem.com/", "Shopping", false, 0},
	// code
	{"gh", "GitHub", "https://github.com/search?q={q}", "https://github.com/", "Code", false, 0},
	{"so", "Stack Overflow", "https://stackoverflow.com/search?q={q}", "https://stackoverflow.com/", "Code", false, 0},
	{"mdn", "MDN Web Docs", "https://developer.mozilla.org/en-US/search?q={q}", "https://developer.mozilla.org/", "Code", false, 0},
	{"pkg", "Go packages", "https://pkg.go.dev/search?q={q}", "https://pkg.go.dev/", "Code", false, 0},
	{"npm", "npm", "https://www.npmjs.com/search?q={q}", "https://www.npmjs.com/", "Code", false, 0},
	{"pypi", "PyPI", "https://pypi.org/search/?q={q}", "https://pypi.org/", "Code", false, 0},
	{"crates", "crates.io", "https://crates.io/search?q={q}", "https://crates.io/", "Code", false, 0},
	{"dh", "Docker Hub", "https://hub.docker.com/search?q={q}", "https://hub.docker.com/", "Code", false, 0},
	{"nix", "NixOS packages", "https://search.nixos.org/packages?query={q}", "https://search.nixos.org/", "Code", false, 0},
	{"aw", "ArchWiki", "https://wiki.archlinux.org/index.php?search={q}", "https://wiki.archlinux.org/", "Code", false, 0},
	{"ciu", "Can I use", "https://caniuse.com/?search={q}", "https://caniuse.com/", "Code", false, 0},
}

var reBangWord = regexp.MustCompile(`^!([a-z0-9][a-z0-9._-]{0,23})$`)

// splitBang finds a bang at the start or the end of a search: ("w", "eiffel tower").
func splitBang(q string) (trigger, rest string, ok bool) {
	f := strings.Fields(q)
	if len(f) == 0 {
		return "", "", false
	}
	if m := reBangWord.FindStringSubmatch(strings.ToLower(f[0])); m != nil {
		return m[1], strings.Join(f[1:], " "), true
	}
	if m := reBangWord.FindStringSubmatch(strings.ToLower(f[len(f)-1])); m != nil && len(f) > 1 {
		return m[1], strings.Join(f[:len(f)-1], " "), true
	}
	return "", "", false
}

// appBangs are "!notes groceries", one per Pocket app with a search.
func appBangs() []bang {
	out := []bang{}
	for _, a := range pocketApps {
		if a.search != nil {
			out = append(out, bang{Trigger: a.bang, Name: a.name, URL: "/search?cat=apps&app=" + a.slug + "&q={q}", Home: "/search?cat=apps&app=" + a.slug, Group: "Your Pocket apps"})
		}
	}
	return out
}

// allBangs is everything a person can use: their own first.
func (s *Server) allBangs(ctx context.Context, uid int64) []bang {
	mine, _ := s.store.ListBangs(ctx, uid)
	out := make([]bang, 0, len(mine)+len(builtinBangs)+16)
	for _, b := range mine {
		out = append(out, bang{Trigger: b.Trigger, Name: b.Name, URL: b.URL, Home: homeOf(b.URL), Group: "Yours", Own: true, ID: b.ID})
	}
	out = append(out, builtinBangs...)
	return append(out, appBangs()...)
}

func homeOf(u string) string {
	if p, err := url.Parse(strings.ReplaceAll(u, "{q}", "")); err == nil && p.Host != "" {
		return p.Scheme + "://" + p.Host + "/"
	}
	return "/"
}

// resolveBang turns a search with a bang into the address to go to ("" = no bang).
func (s *Server) resolveBang(ctx context.Context, uid int64, q string) string {
	trigger, rest, ok := splitBang(q)
	if !ok {
		return ""
	}
	if own, err := s.store.FindBang(ctx, uid, trigger); err == nil {
		s.store.UsedBang(ctx, uid, own.ID)
		return fill(bang{URL: own.URL, Home: homeOf(own.URL)}, rest)
	}
	for _, list := range [][]bang{builtinBangs, appBangs()} {
		for _, b := range list {
			if b.Trigger == trigger {
				return fill(b, rest)
			}
		}
	}
	return ""
}

func fill(b bang, words string) string {
	if strings.TrimSpace(words) == "" {
		return b.Home
	}
	esc := url.QueryEscape(words)
	if strings.Contains(b.URL, "/wiki/") || strings.Contains(b.URL, "/maps/search/") || strings.Contains(b.URL, "letterboxd.com/search/") || strings.Contains(b.URL, "#auto/") {
		esc = url.PathEscape(words)
	}
	if strings.HasPrefix(b.URL, "https://web.archive.org/") {
		esc = words
	}
	return strings.ReplaceAll(b.URL, "{q}", esc)
}

// validBang checks a person's own bang.
func validBang(b *store.Bang) string {
	b.Trigger = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(b.Trigger), "!"))
	b.Name = strings.TrimSpace(b.Name)
	b.URL = strings.TrimSpace(b.URL)
	if !reBangWord.MatchString("!" + b.Trigger) {
		return "A shortcut is 1–24 letters, digits, dots or dashes (like yt or kp)"
	}
	if !strings.Contains(b.URL, "{q}") {
		return "The address needs {q} where the search words go"
	}
	u, err := url.Parse(strings.ReplaceAll(b.URL, "{q}", "x"))
	if err != nil || ((u.Scheme != "http" && u.Scheme != "https") || u.Host == "") && !strings.HasPrefix(b.URL, "/") {
		return "The address must start with https:// (or / for a page of Pocket Search)"
	}
	if b.Name == "" {
		b.Name = strings.TrimPrefix(u.Hostname(), "www.")
	}
	return ""
}
