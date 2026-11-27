package server

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// ---- apps that say themselves that they can be searched ----
//
// An app that declares the search capability in its manifest
// (docs/capabilities.md in pocket-homelab-core) answers on one fixed route in
// one shape: Search asks Core which apps offer pocket:search and how the link
// to each stands for the person, and searches them through the contract,
// with the bangs their manifests bring. That is the only way Search searches
// a Pocket app; apps.go only says where a result can be sent and where an
// instant answer comes from. A person who reaches one of those apps directly
// (their own address and token, also without Pocket Core) has it searched on
// the same route. Without Pocket Core, or with one that does not know
// capabilities yet, nothing is found through Core.

const (
	capableTTL  = 30 * time.Second // how long a person's list is kept
	capableMiss = 10 * time.Second // after Core did not answer: not asked again on every search
	capableMax  = 500              // people remembered at once
	capBangsMax = 5                // shortcuts taken from one app's manifest
)

type capableEntry struct {
	apps  []core.CapableApp
	until time.Time
}

// capableApps are the apps that offer the search capability and how the link
// to each stands for the person, sorted by name. Kept for half a minute per
// person; callers must not change the list. Nil without Pocket Core or when
// it does not answer: the search never fails over this.
func (s *Server) capableApps(ctx context.Context, u *store.User) []core.CapableApp {
	if u == nil || u.Email == "" || !s.core.Configured() {
		return nil
	}
	key := strings.ToLower(u.Email)
	s.capMu.Lock()
	e, ok := s.capable[key]
	s.capMu.Unlock()
	if ok && time.Now().Before(e.until) {
		return e.apps
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	list, err := s.core.Capable(cctx, u.Email, core.ScopeSearch)
	keep := capableTTL
	if err != nil {
		if ctx.Err() != nil {
			return nil // the person went away; nothing learned
		}
		s.log.Debug("pocket core: asking which apps can be searched failed", "err", err)
		list, keep = nil, capableMiss
	}
	apps := make([]core.CapableApp, 0, len(list))
	seen := map[string]bool{}
	for _, a := range list {
		if a.App == "" || a.App == "search" || seen[a.App] {
			continue
		}
		seen[a.App] = true
		if !strings.HasPrefix(a.Icon, "https://") && !strings.HasPrefix(a.Icon, "http://") {
			a.Icon = "" // shown as a picture: an address or nothing
		}
		apps = append(apps, a)
	}
	sort.SliceStable(apps, func(i, j int) bool {
		ni, nj := strings.ToLower(capName(apps[i])), strings.ToLower(capName(apps[j]))
		if ni != nj {
			return ni < nj
		}
		return apps[i].App < apps[j].App
	})
	s.capMu.Lock()
	if len(s.capable) >= capableMax {
		now := time.Now()
		for k, v := range s.capable {
			if now.After(v.until) {
				delete(s.capable, k)
			}
		}
		if len(s.capable) >= capableMax {
			s.capable = map[string]capableEntry{}
		}
	}
	s.capable[key] = capableEntry{apps: apps, until: time.Now().Add(keep)}
	s.capMu.Unlock()
	return apps
}

// forgetCapable drops a person's kept list, so the next search asks Core again
// (a link turned out to be off while the list still said on).
func (s *Server) forgetCapable(u *store.User) {
	if u == nil {
		return
	}
	s.capMu.Lock()
	delete(s.capable, strings.ToLower(u.Email))
	s.capMu.Unlock()
}

// capableBySlug indexes a person's list.
func capableBySlug(apps []core.CapableApp) map[string]core.CapableApp {
	out := make(map[string]core.CapableApp, len(apps))
	for _, a := range apps {
		out[a.App] = a
	}
	return out
}

// capableApp is one app that offers the search capability for the person.
func (s *Server) capableApp(ctx context.Context, u *store.User, slug string) (core.CapableApp, bool) {
	for _, a := range s.capableApps(ctx, u) {
		if a.App == slug {
			return a, true
		}
	}
	return core.CapableApp{}, false
}

// capName is the name people know the app by.
func capName(a core.CapableApp) string {
	return firstNonEmpty(strings.TrimSpace(a.Name), a.App)
}

// capPublic is the address a browser opens the app at ("" when Core has none).
func capPublic(a core.CapableApp) string {
	return strings.TrimRight(firstNonEmpty(a.PublicURL, a.URL), "/")
}

// capReason says what Search finds in the app: the app's own words, else a plain sentence.
func capReason(a core.CapableApp) string {
	if a.Search != nil && strings.TrimSpace(a.Search.What) != "" {
		return oneLine(a.Search.What, 200)
	}
	return "Search what you keep in " + capName(a) + " beside the web"
}

// capOffText says in plain words why the link to the app does not work for
// the person (the same sentences as for the table's own links).
func capOffText(name, state, detail string) string {
	if t := linkReason(name, state); t != "" {
		return t
	}
	return firstNonEmpty(detail, state, "the link is not switched on in Pocket Core")
}

// discoveredMode is the mode in effect for an app found through Core and not
// in the table: Pocket Core, unless the person switched it off here. There
// is no direct connection.
func discoveredMode(c store.AppConn) string {
	if c.Mode == modeOff {
		return modeOff
	}
	return modeCore
}

// ---- searching through the contract ----

// searchTarget is one app to search for a person: an app that offers the
// search capability, one of the table's apps the person reaches directly, or
// both.
type searchTarget struct {
	slug  string
	table *pocketApp       // nil = not in the table
	cap   *core.CapableApp // nil = not offered through Pocket Core
}

// searchTargets lists what a search of the person's apps covers: the apps
// that offer the search capability, by name, then the table's apps the
// person reaches directly that Core did not name (without Pocket Core: all
// of them). An app the person left out of the results is skipped unless it
// was asked for by name.
func (s *Server) searchTargets(ctx context.Context, u *store.User, st store.Settings, only string) []searchTarget {
	capable := s.capableApps(ctx, u)
	wanted := func(slug string) bool {
		if only != "" {
			return slug == only
		}
		c := st.Apps[slug]
		return c.Search == nil || *c.Search
	}
	var out []searchTarget
	named := map[string]bool{}
	for i := range capable {
		c := capable[i]
		named[c.App] = true
		if !wanted(c.App) {
			continue
		}
		t := searchTarget{slug: c.App, cap: &c}
		for j := range pocketApps {
			if pocketApps[j].slug == c.App {
				t.table = &pocketApps[j]
			}
		}
		out = append(out, t)
	}
	for i := range pocketApps {
		a := &pocketApps[i]
		c := st.Apps[a.slug]
		if named[a.slug] || !wanted(a.slug) || s.connMode(c.Mode, c.Token) != modeDirect {
			continue
		}
		out = append(out, searchTarget{slug: a.slug, table: a})
	}
	return out
}

// searchOne searches one app the way that fits it for the person: directly
// when they connected it that way, else through Pocket Core.
func (s *Server) searchOne(ctx context.Context, u *store.User, st store.Settings, t searchTarget, q string, limit int) appSection {
	c := st.Apps[t.slug]
	if t.table != nil && s.connMode(c.Mode, c.Token) == modeDirect {
		return s.searchDirect(ctx, u, st, *t.table, q, limit)
	}
	name := capName(*t.cap)
	if t.table != nil {
		name = t.table.name
	}
	sec := appSection{App: t.slug, Name: name, Icon: t.cap.Icon, Items: []appItem{}}
	if c.Mode == modeOff {
		sec.Error, sec.Off = name+" is switched off in Settings → Connected apps", true
		return sec
	}
	if !t.cap.On {
		sec.Error, sec.Off = name+" is not set up: "+capOffText(name, t.cap.State, t.cap.Detail), true
		return sec
	}
	sec, linkOff := s.searchCapable(ctx, u, *t.cap, name, t.cap.Icon, c.Public, q, limit)
	if linkOff {
		// switched off since the list was read
		s.forgetCapable(u)
	}
	return sec
}

// searchCapable searches an app through the search capability and maps its
// answer. ownPublic is the person's own browser address for the app, when
// they set one. linkOff reports that Core refused the link (the section then
// says why, as "not connected").
func (s *Server) searchCapable(ctx context.Context, u *store.User, a core.CapableApp, name, icon, ownPublic, q string, limit int) (sec appSection, linkOff bool) {
	sec = appSection{App: a.App, Name: name, Icon: icon, Items: []appItem{}}
	start := time.Now()
	defer func() { sec.MS = time.Since(start).Milliseconds() }()
	items, err := s.core.SearchIn(ctx, u.Email, a.App, q, limit)
	if err != nil {
		var le *core.LinkError
		var ce *core.CapabilityError
		switch {
		case errors.As(err, &le):
			sec.Error, sec.Off = name+" is not set up: "+capOffText(name, le.Reason, le.Error()), true
			return sec, true
		case errors.As(err, &ce):
			sec.Error = fmt.Sprintf("%s did not answer: %s (HTTP %d)", name, ce.Message, ce.Status)
		default:
			sec.Error = name + " did not answer: " + strings.TrimPrefix(err.Error(), a.App+" did not answer: ")
		}
		return sec, false
	}
	public := capPublic(a)
	ownPublic = strings.TrimRight(ownPublic, "/")
	kind := ""
	if a.Search != nil && len(a.Search.Kinds) == 1 {
		kind = a.Search.Kinds[0] // an app with one kind of thing need not repeat it on every item
	}
	sec.Items = appItems(items, public, ownPublic, kind, q, limit)
	if more := firstNonEmpty(ownPublic, public); more != "" {
		sec.More = more + "/"
	}
	return sec, false
}

// appItems are an app's matches as shown, at most limit of them: the app's
// own order unless it scored them (then higher first), an item without a
// title or address left out, an item without a page opening the app
// (public), and the person's own browser address for the app (ownPublic)
// put in front of its pages when they set one.
func appItems(items []core.SearchItem, public, ownPublic, kind, q string, limit int) []appItem {
	for _, it := range items {
		if it.Score != 0 {
			sort.SliceStable(items, func(i, j int) bool { return items[i].Score > items[j].Score })
			break
		}
	}
	out := []appItem{}
	for _, x := range items {
		it := appItem{Title: strings.TrimSpace(x.Title), Snippet: cleanSnippet(x.Snippet), Date: x.Date, URL: x.URL, Link: x.Link, Kind: firstNonEmpty(x.Kind, kind)}
		if it.Title == "" {
			it.Title = it.Link
		}
		if it.Title == "" {
			continue
		}
		if it.URL == "" && public != "" {
			it.URL = public + "/"
		}
		if ownPublic != "" && public != "" && ownPublic != public && strings.HasPrefix(it.URL, public+"/") {
			it.URL = ownPublic + it.URL[len(public):]
		}
		it.Strong = strongMatch(it.Title, q)
		out = append(out, it)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// ---- their bangs ----

var reCapBang = regexp.MustCompile(`^[a-z0-9]{2,20}$`)

// takenBangs are the shortcuts that already mean something: the built-in ones.
var takenBangs = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	for _, b := range builtinBangs {
		out[b.Trigger] = true
	}
	return out
})

// capBangs gives each app that offers the search capability the shortcuts
// from its manifest that are free: lowercase letters and digits, 2–20 long,
// not a built-in bang, not already taken by an app earlier in the list. An
// app's own slug is its to use, whoever else asks for it, and so are the
// table's slugs. The others are left out without a word.
func capBangs(apps []core.CapableApp) map[string][]string {
	free := func(b string) bool { return reCapBang.MatchString(b) && !takenBangs()[b] }
	owner := map[string]string{}
	for _, a := range pocketApps {
		owner[a.slug] = a.slug
	}
	for _, a := range apps {
		if free(a.App) {
			owner[a.App] = a.App
		}
	}
	out := map[string][]string{}
	for _, a := range apps {
		if a.Search == nil {
			continue
		}
		for _, b := range a.Search.Bangs {
			b = strings.TrimSpace(b)
			if !free(b) || len(out[a.App]) >= capBangsMax {
				continue
			}
			if o, taken := owner[b]; taken && (o != a.App || contains(out[a.App], b)) {
				continue
			}
			owner[b] = a.App
			out[a.App] = append(out[a.App], b)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// appBangs are "!recipes bread" for the person's Pocket apps: the bangs from
// the manifests of the apps that offer the search capability, and its slug
// ("!notes") for a table app the person reaches directly that Core did not
// name (without Pocket Core, the only ones).
func (s *Server) appBangs(ctx context.Context, u *store.User, st store.Settings) []bang {
	apps := s.capableApps(ctx, u)
	bangs := capBangs(apps)
	var out []bang
	named := map[string]bool{}
	add := func(slug, name, trigger string) {
		to := "/search?cat=apps&app=" + url.QueryEscape(slug)
		out = append(out, bang{Trigger: trigger, Name: name, URL: to + "&q={q}", Home: to, Group: "Your Pocket apps"})
	}
	for _, a := range apps {
		named[a.App] = true
		name := capName(a)
		if t, ok := appBySlug(a.App); ok {
			name = t.name
		}
		for _, t := range bangs[a.App] {
			add(a.App, name, t)
		}
	}
	for _, a := range pocketApps {
		c := st.Apps[a.slug]
		if !named[a.slug] && s.connMode(c.Mode, c.Token) == modeDirect && !takenBangs()[a.slug] {
			add(a.slug, a.name, a.slug)
		}
	}
	return out
}
