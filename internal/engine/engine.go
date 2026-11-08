// Package engine talks to SearXNG, the meta-search engine that asks ~70
// search engines at once. Pocket Search keeps SearXNG private (no port of its
// own, only this app reaches it) and uses its JSON format; everything a person
// sees — ranking rules, apps, history, the reading view — is Pocket Search's.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Searcher is what the server needs from an engine (SearXNG, or the demo one).
type Searcher interface {
	Search(ctx context.Context, q Query) (*Response, error)
	Suggest(ctx context.Context, q, lang string) ([]string, error)
	Engines(ctx context.Context) ([]Info, error)
	Configured() bool
}

// Query is one search.
type Query struct {
	Q          string
	Category   string // general | images | videos | news | …
	Page       int    // 1-based
	TimeRange  string // "" | day | week | month | year
	Language   string // "auto" | "en" | "de-DE" …
	SafeSearch int    // 0 | 1 | 2
	Engines    []string
}

// Result is one hit.
type Result struct {
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Engine     string   `json:"engine"`
	Engines    []string `json:"engines"`
	Score      float64  `json:"score"`
	Category   string   `json:"category"`
	Published  string   `json:"published,omitempty"`
	Thumbnail  string   `json:"thumbnail,omitempty"` // small picture (videos, news, images)
	Image      string   `json:"image,omitempty"`     // the full picture (images)
	Length     string   `json:"length,omitempty"`    // a video's duration
	Author     string   `json:"author,omitempty"`
	Metadata   string   `json:"metadata,omitempty"` // "11 days ago | Lead Stories"
	Resolution string   `json:"resolution,omitempty"`
	Source     string   `json:"source,omitempty"` // an image's page host
}

// Link is a titled address.
type Link struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Official bool   `json:"official,omitempty"`
}

// Attr is one row of an infobox.
type Attr struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Infobox is the card about the thing searched for (Wikipedia/Wikidata).
type Infobox struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Image   string `json:"image,omitempty"`
	URL     string `json:"url,omitempty"`
	Links   []Link `json:"links"`
	Attrs   []Attr `json:"attributes"`
}

// Answer is a direct answer (a calculation, a conversion, a definition).
type Answer struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// Unresponsive is an engine that did not answer, and why.
type Unresponsive struct {
	Engine string `json:"engine"`
	Reason string `json:"reason"`
}

// Response is what one search brought back.
type Response struct {
	Results      []Result       `json:"results"`
	Answers      []Answer       `json:"answers"`
	Infoboxes    []Infobox      `json:"infoboxes"`
	Suggestions  []string       `json:"suggestions"`
	Corrections  []string       `json:"corrections"`
	Unresponsive []Unresponsive `json:"unresponsive"`
}

// Info describes one engine SearXNG knows.
type Info struct {
	Name       string   `json:"name"`
	Shortcut   string   `json:"shortcut"`
	Categories []string `json:"categories"`
	Enabled    bool     `json:"enabled"` // on by default
	Paging     bool     `json:"paging"`
	TimeRange  bool     `json:"time_range"`
	SafeSearch bool     `json:"safe_search"`
}

// ErrNotConfigured means there is no engine (SEARXNG_URL is empty).
var ErrNotConfigured = errors.New("no search engine is set up (SEARXNG_URL)")

// SearXNG is a client for one SearXNG instance.
type SearXNG struct {
	base string
	http *http.Client
	ua   string
}

// New returns a client for the SearXNG at base ("" = not configured).
func New(base, userAgent string, timeout time.Duration) *SearXNG {
	return &SearXNG{base: strings.TrimRight(base, "/"), ua: userAgent, http: &http.Client{Timeout: timeout + 4*time.Second}}
}

// Configured reports whether there is an engine.
func (c *SearXNG) Configured() bool { return c != nil && c.base != "" }

func (c *SearXNG) get(ctx context.Context, path string, v url.Values, out any) error {
	if !c.Configured() {
		return ErrNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+v.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.ua)
	// SearXNG's bot checks look at these; the request comes from the app, not a bot
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "127.0.0.1")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("the search engine did not answer: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	switch {
	case res.StatusCode == http.StatusForbidden:
		return errors.New("the search engine refused JSON: switch on the json format in its settings.yml (search.formats)")
	case res.StatusCode == http.StatusTooManyRequests:
		return errors.New("the search engine is rate limiting this app: switch its limiter off (server.limiter: false)")
	case res.StatusCode >= 300:
		return fmt.Errorf("the search engine answered %s", res.Status)
	}
	return json.Unmarshal(body, out)
}

// raw is SearXNG's JSON.
type raw struct {
	Results []struct {
		URL           string   `json:"url"`
		Title         string   `json:"title"`
		Content       string   `json:"content"`
		Engine        string   `json:"engine"`
		Engines       []string `json:"engines"`
		Score         float64  `json:"score"`
		Category      string   `json:"category"`
		PublishedDate *string  `json:"publishedDate"`
		Thumbnail     *string  `json:"thumbnail"`
		ThumbnailSrc  string   `json:"thumbnail_src"`
		ImgSrc        string   `json:"img_src"`
		Length        any      `json:"length"`
		Author        string   `json:"author"`
		Metadata      string   `json:"metadata"`
		Resolution    string   `json:"resolution"`
		Source        string   `json:"source"`
	} `json:"results"`
	Answers   []json.RawMessage `json:"answers"`
	Infoboxes []struct {
		Infobox string `json:"infobox"`
		ID      string `json:"id"`
		Content string `json:"content"`
		ImgSrc  string `json:"img_src"`
		URLs    []struct {
			Title    string `json:"title"`
			URL      string `json:"url"`
			Official bool   `json:"official"`
		} `json:"urls"`
		Attributes []struct {
			Label string `json:"label"`
			Value any    `json:"value"`
		} `json:"attributes"`
	} `json:"infoboxes"`
	Suggestions  []string `json:"suggestions"`
	Corrections  []string `json:"corrections"`
	Unresponsive [][]any  `json:"unresponsive_engines"`
}

// marks are SearXNG's highlight brackets around matched words.
var marks = strings.NewReplacer("", "", "", "")

func clean(s string) string { return strings.TrimSpace(marks.Replace(s)) }

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// Search runs one search.
func (c *SearXNG) Search(ctx context.Context, q Query) (*Response, error) {
	v := url.Values{"q": {q.Q}, "format": {"json"}}
	// SearXNG adds every engine of a category asked for to the engines named,
	// so a search naming its engines names no category
	if q.Category != "" && len(q.Engines) == 0 {
		v.Set("categories", q.Category)
	}
	if q.Page > 1 {
		v.Set("pageno", strconv.Itoa(q.Page))
	}
	if q.TimeRange != "" {
		v.Set("time_range", q.TimeRange)
	}
	if q.Language != "" {
		v.Set("language", q.Language)
	}
	v.Set("safesearch", strconv.Itoa(q.SafeSearch))
	if len(q.Engines) > 0 {
		v.Set("engines", strings.Join(q.Engines, ","))
	}
	var r raw
	if err := c.get(ctx, "/search", v, &r); err != nil {
		return nil, err
	}
	return r.response(), nil
}

func (r *raw) response() *Response {
	out := &Response{Results: []Result{}, Answers: []Answer{}, Infoboxes: []Infobox{}, Suggestions: []string{}, Corrections: []string{}, Unresponsive: []Unresponsive{}}
	for _, x := range r.Results {
		if x.URL == "" {
			continue
		}
		res := Result{URL: x.URL, Title: clean(x.Title), Content: clean(x.Content), Engine: x.Engine, Engines: x.Engines, Score: x.Score,
			Category: x.Category, Author: x.Author, Metadata: x.Metadata, Resolution: x.Resolution, Source: x.Source, Length: str(x.Length)}
		if res.Engines == nil {
			res.Engines = []string{x.Engine}
		}
		if x.PublishedDate != nil {
			res.Published = *x.PublishedDate
		}
		if x.Thumbnail != nil {
			res.Thumbnail = *x.Thumbnail
		}
		if res.Thumbnail == "" {
			res.Thumbnail = x.ThumbnailSrc
		}
		res.Image = x.ImgSrc
		if res.Title == "" {
			res.Title = x.URL
		}
		out.Results = append(out.Results, res)
	}
	for _, a := range r.Answers {
		var s string
		if json.Unmarshal(a, &s) == nil {
			out.Answers = append(out.Answers, Answer{Text: clean(s)})
			continue
		}
		var o struct {
			Answer string `json:"answer"`
			URL    string `json:"url"`
		}
		if json.Unmarshal(a, &o) == nil && o.Answer != "" {
			out.Answers = append(out.Answers, Answer{Text: clean(o.Answer), URL: o.URL})
		}
	}
	for _, b := range r.Infoboxes {
		ib := Infobox{Title: b.Infobox, Content: clean(b.Content), Image: b.ImgSrc, URL: b.ID, Links: []Link{}, Attrs: []Attr{}}
		for _, u := range b.URLs {
			if u.URL != "" {
				ib.Links = append(ib.Links, Link{Title: u.Title, URL: u.URL, Official: u.Official})
			}
		}
		for _, a := range b.Attributes {
			if s := str(a.Value); a.Label != "" && s != "" && len(s) < 300 {
				ib.Attrs = append(ib.Attrs, Attr{Label: a.Label, Value: s})
			}
		}
		if !strings.HasPrefix(ib.URL, "http") {
			ib.URL = ""
		}
		out.Infoboxes = append(out.Infoboxes, ib)
	}
	out.Suggestions = append(out.Suggestions, r.Suggestions...)
	out.Corrections = append(out.Corrections, r.Corrections...)
	for _, u := range r.Unresponsive {
		if len(u) >= 2 {
			out.Unresponsive = append(out.Unresponsive, Unresponsive{Engine: str(u[0]), Reason: str(u[1])})
		}
	}
	return out
}

// Suggest returns search suggestions for what is typed so far.
func (c *SearXNG) Suggest(ctx context.Context, q, lang string) ([]string, error) {
	v := url.Values{"q": {q}}
	if lang != "" && lang != "auto" {
		v.Set("language", lang)
	}
	// OpenSearch shape: [query, [suggestions], …]
	var r []json.RawMessage
	if err := c.get(ctx, "/autocompleter", v, &r); err != nil {
		return nil, err
	}
	out := []string{}
	if len(r) >= 2 {
		_ = json.Unmarshal(r[1], &out)
	}
	return out, nil
}

// Engines lists the engines SearXNG knows, sorted by name.
func (c *SearXNG) Engines(ctx context.Context) ([]Info, error) {
	var r struct {
		Engines []struct {
			Name             string   `json:"name"`
			Shortcut         string   `json:"shortcut"`
			Categories       []string `json:"categories"`
			Enabled          bool     `json:"enabled"`
			Paging           bool     `json:"paging"`
			TimeRangeSupport bool     `json:"time_range_support"`
			SafeSearch       bool     `json:"safesearch"`
		} `json:"engines"`
	}
	if err := c.get(ctx, "/config", url.Values{}, &r); err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(r.Engines))
	for _, e := range r.Engines {
		out = append(out, Info{Name: e.Name, Shortcut: e.Shortcut, Categories: e.Categories, Enabled: e.Enabled, Paging: e.Paging, TimeRange: e.TimeRangeSupport, SafeSearch: e.SafeSearch})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Pick is the engine list for one category given a person's choices: nil
// when they changed nothing there (SearXNG's own defaults apply).
func Pick(all []Info, category string, off, on []string) []string {
	offSet, onSet := set(off), set(on)
	changed := false
	var out []string
	for _, e := range all {
		if !has(e.Categories, category) {
			continue
		}
		use := e.Enabled
		if e.Enabled && offSet[e.Name] {
			use, changed = false, true
		}
		if !e.Enabled && onSet[e.Name] {
			use, changed = true, true
		}
		if use {
			out = append(out, e.Name)
		}
	}
	if !changed {
		return nil
	}
	if out == nil {
		out = []string{} // every engine switched off: not "the defaults"
	}
	return out
}

func set(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, s := range list {
		m[s] = true
	}
	return m
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
