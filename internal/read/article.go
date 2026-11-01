package read

import (
	"bytes"
	"context"
	"errors"
	"html"
	"mime"
	"net/url"
	"regexp"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/microcosm-cc/bluemonday"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Article is the readable part of a web page.
type Article struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Byline    string `json:"byline"`
	Site      string `json:"site"`
	Excerpt   string `json:"excerpt"`
	Image     string `json:"image"`
	Lang      string `json:"lang"`
	Published string `json:"published"`
	Content   string `json:"content"` // cleaned HTML, pictures through the proxy
	Text      string `json:"-"`       // plain text (for AI answers)
	Words     int    `json:"words"`
	Minutes   int    `json:"minutes"`
	Embeds    int    `json:"embeds"` // videos and players left out
}

// ErrNoArticle means the page has no readable article (a home page, an app, a paywall).
var ErrNoArticle = errors.New("this page has no article to read here (it may be a home page, an app or behind a paywall); open the original instead")

// Extract downloads a page and keeps its readable article. img rewrites
// every picture address (to the app's proxy); nil drops pictures.
func (f *Fetcher) Extract(ctx context.Context, raw string, img func(string) string) (*Article, error) {
	p, err := f.get(ctx, raw, "text/html,application/xhtml+xml;q=0.9,text/plain;q=0.5", 8<<20)
	if err != nil {
		return nil, err
	}
	mt, _, _ := mime.ParseMediaType(p.ContentType)
	if mt != "" && !strings.Contains(mt, "html") && !strings.Contains(mt, "xml") && mt != "text/plain" {
		return nil, errors.New("that link is not a web page (" + mt + "); open the original instead")
	}
	return ExtractPage(p.Body, p.URL, img)
}

var (
	reTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reMeta  = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	reAttr  = regexp.MustCompile(`(?is)([a-z:_-]+)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	reTags  = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpace = regexp.MustCompile(`\s+`)
)

// ExtractPage keeps the readable article of a downloaded page.
func ExtractPage(page []byte, pageURL string, img func(string) string) (*Article, error) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	a := &Article{URL: pageURL}
	a.Title, a.Excerpt, a.Image, a.Site = pageMeta(string(page), base)
	art, err := readability.FromReader(bytes.NewReader(page), base)
	if err != nil || art.Node == nil {
		return a, ErrNoArticle
	}
	var buf bytes.Buffer
	if err := art.RenderHTML(&buf); err != nil {
		return a, ErrNoArticle
	}
	a.Content, a.Embeds = Sanitize(buf.String(), base, img)
	a.Text = PlainText(a.Content)
	if len(a.Text) < 200 {
		a.Content, a.Text = "", ""
		return a, ErrNoArticle
	}
	a.Title = first(clean(art.Title()), a.Title)
	a.Byline = clean(art.Byline())
	a.Site = first(clean(art.SiteName()), a.Site)
	a.Excerpt = first(a.Excerpt, Excerpt(clean(art.Excerpt()), 300))
	a.Image = first(a.Image, absURL(base, art.ImageURL()))
	if a.Image != "" && img != nil {
		a.Image = img(a.Image)
	} else if img == nil {
		a.Image = ""
	}
	a.Lang = art.Language()
	if t, err := art.PublishedTime(); err == nil && !t.IsZero() {
		a.Published = t.UTC().Format(time.RFC3339)
	}
	a.Words = len(strings.Fields(a.Text))
	a.Minutes = max(1, (a.Words+219)/220)
	return a, nil
}

func pageMeta(page string, base *url.URL) (title, excerpt, image, site string) {
	meta := map[string]string{}
	for _, tag := range reMeta.FindAllString(page[:min(len(page), 400_000)], -1) {
		at := map[string]string{}
		for _, m := range reAttr.FindAllStringSubmatch(tag, -1) {
			at[strings.ToLower(m[1])] = html.UnescapeString(strings.Trim(m[2], `"'`))
		}
		k := strings.ToLower(first(at["property"], at["name"]))
		if k != "" && at["content"] != "" {
			if _, ok := meta[k]; !ok {
				meta[k] = strings.TrimSpace(at["content"])
			}
		}
	}
	title = first(meta["og:title"], meta["twitter:title"])
	if title == "" {
		if m := reTitle.FindStringSubmatch(page); m != nil {
			title = m[1]
		}
	}
	excerpt = Excerpt(first(meta["og:description"], meta["description"], meta["twitter:description"]), 300)
	image = absURL(base, first(meta["og:image"], meta["twitter:image"]))
	site = first(meta["og:site_name"], meta["application-name"])
	if site == "" && base != nil {
		site = strings.TrimPrefix(base.Hostname(), "www.")
	}
	return clean(title), clean(excerpt), image, clean(site)
}

var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowElements("figure", "figcaption", "picture", "details", "summary", "mark", "sup", "sub", "abbr", "time")
	p.AllowAttrs("loading", "decoding").OnElements("img")
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true) // the picture proxy is /api/v1/img?…
	return p
}()

// Sanitize makes an article safe and private to show: scripts, styles, forms,
// trackers, players and embeds go (they would load from the site); links and
// pictures become absolute and pictures go through img. It returns the HTML
// and how many players/embeds were left out.
func Sanitize(h string, base *url.URL, img func(string) string) (string, int) {
	if strings.TrimSpace(h) == "" {
		return "", 0
	}
	h, embeds := rewrite(h, base, img)
	return strings.TrimSpace(policy.Sanitize(h)), embeds
}

func rewrite(h string, base *url.URL, img func(string) string) (string, int) {
	nodes, err := xhtml.ParseFragment(strings.NewReader(h), &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return h, 0
	}
	embeds := 0
	var drop []*xhtml.Node
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "form", "button", "input", "select", "textarea", "object", "embed", "link", "meta":
				drop = append(drop, n)
				return
			case "iframe", "video", "audio":
				embeds++
				drop = append(drop, n)
				return
			case "source":
				drop = append(drop, n)
				return
			case "img":
				src := attr(n, "src")
				for _, k := range []string{"data-src", "data-lazy-src", "data-original"} {
					if v := attr(n, k); v != "" && (src == "" || strings.HasPrefix(src, "data:")) {
						src = v
						break
					}
				}
				w, hh := attr(n, "width"), attr(n, "height")
				src = absURL(base, src)
				if img == nil || src == "" || (w == "1" && hh == "1") || strings.Contains(src, "/pixel") {
					drop = append(drop, n)
					return
				}
				n.Attr = keep(n.Attr, "alt", "title", "width", "height")
				setAttr(n, "src", img(src))
				setAttr(n, "loading", "lazy")
				setAttr(n, "decoding", "async")
			case "a":
				if href := attr(n, "href"); href != "" && !strings.HasPrefix(href, "#") && !strings.HasPrefix(href, "mailto:") {
					setAttr(n, "href", first(absURL(base, href), href))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	dropped := map[*xhtml.Node]bool{}
	for _, n := range drop {
		dropped[n] = true
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}
	var buf bytes.Buffer
	for _, n := range nodes {
		if !dropped[n] {
			_ = xhtml.Render(&buf, n)
		}
	}
	return buf.String(), embeds
}

func keep(attrs []xhtml.Attribute, names ...string) []xhtml.Attribute {
	var out []xhtml.Attribute
	for _, a := range attrs {
		for _, n := range names {
			if a.Key == n {
				out = append(out, a)
			}
		}
	}
	return out
}

func attr(n *xhtml.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *xhtml.Node, k, v string) {
	for i, a := range n.Attr {
		if a.Key == k {
			n.Attr[i].Val = v
			return
		}
	}
	n.Attr = append(n.Attr, xhtml.Attribute{Key: k, Val: v})
}

func absURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func clean(s string) string {
	return strings.TrimSpace(reSpace.ReplaceAllString(html.UnescapeString(reTags.ReplaceAllString(s, " ")), " "))
}

// PlainText turns HTML into text, one paragraph per line.
func PlainText(h string) string {
	h = strings.NewReplacer("</p>", "\n</p>", "<br", "\n<br", "</li>", "\n</li>", "</h1>", "\n</h1>", "</h2>", "\n</h2>", "</h3>", "\n</h3>").Replace(h)
	var lines []string
	for _, l := range strings.Split(html.UnescapeString(reTags.ReplaceAllString(h, " ")), "\n") {
		if l = strings.TrimSpace(reSpace.ReplaceAllString(l, " ")); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// Excerpt is the first n runes of text, cut at a word.
func Excerpt(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	cut := string(r[:n])
	if i := strings.LastIndex(cut, " "); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-–—") + "…"
}

func first(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
