package server

import (
	"fmt"
	"net/url"
	"strings"
)

// pocketApps are the Pocket apps Search searches beside the web ("From your
// apps", and !<bang> for one app) and the ones a result can be sent to. Every
// app is reached with only the scope that work needs; each link starts off
// in Pocket Core until the person (or an admin) switches it on.
//
// route: the item's page in the app; {field} is filled from the row.
var pocketApps = []pocketApp{
	{slug: "notes", name: "Pocket Notes", bang: "notes", reason: "Search your notes beside the web, and keep a result as a note (title, link and snippet)",
		search: &appSearch{scope: "notes:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}},
			id: f("id"), title: f("title"), snippet: f("snippet", "excerpt"), date: f("updated_at", "modified"), route: "/note/{id}", kind: "note"},
		send: &appSend{scope: "notes:write", label: "Save as a note", done: "Saved to Pocket Notes", path: "/api/v1/notes",
			body: func(in sendIn) map[string]any {
				return map[string]any{"title": in.Title, "category": "Web", "content": noteMarkdown(in)}
			}, id: f("id"), route: "/note/{id}"}},
	{slug: "reader", name: "Pocket Reader", bang: "reader", reason: "Search your saved articles beside the web, and save a result to read later",
		search: &appSearch{scope: "reading:read", path: "/api/v1/articles", q: "q", extra: url.Values{"limit": {"8"}}, list: "items",
			id: f("id"), title: f("title"), snippet: f("excerpt", "site_name"), date: f("added_at", "published_at"), link: f("url"), route: "/a/{id}", kind: "article"},
		send: &appSend{scope: "reading:write", label: "Read later in Reader", done: "Saved to Pocket Reader", path: "/api/v1/saved",
			body: func(in sendIn) map[string]any {
				return map[string]any{"url": in.URL, "title": in.Title, "note": in.Note}
			},
			id: f("article.id"), route: "/a/{id}"}},
	{slug: "tasks", name: "Pocket Tasks", bang: "tasks", reason: "Search your tasks beside the web, and turn a result into a task with its link",
		search: &appSearch{scope: "tasks:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}},
			id: f("id"), title: f("title"), snippet: f("notes"), date: f("due"), kind: "task"},
		send: &appSend{scope: "tasks:write", label: "Add as a task", done: "Added to Pocket Tasks", path: "/api/v1/tasks",
			body: func(in sendIn) map[string]any {
				return map[string]any{"title": in.Title, "url": in.URL, "notes": strings.TrimSpace(in.Snippet + "\n\n" + in.URL)}
			}, id: f("id")}},
	{slug: "lookout", name: "Pocket Lookout", bang: "lookout", reason: "Search the pages you watch, and start watching a result for changes",
		search: &appSearch{scope: "watches:read", path: "/api/v1/watches", q: "q",
			id: f("id"), title: f("name", "title", "page_title"), snippet: f("last_summary", "url"), date: f("last_changed_at"), link: f("url"), route: "/watches/{id}", kind: "watched page"},
		send: &appSend{scope: "watches:write", label: "Watch for changes", done: "Pocket Lookout watches it now", path: "/api/v1/watches",
			body: func(in sendIn) map[string]any { return map[string]any{"url": in.URL, "title": in.Title} },
			id:   f("id"), route: "/watches/{id}"}},
	{slug: "wishlist", name: "Pocket Wishlist", bang: "wish", reason: "Search your wishes, and add a product you found to your wishlist (price read from the shop)",
		search: &appSearch{scope: "wishes:read", path: "/api/v1/wishes", q: "q",
			id: f("id"), title: f("title"), snippet: f("store", "notes"), link: f("url"), route: "/wishes/{id}", kind: "wish"},
		send: &appSend{scope: "wishes:write", label: "Add to wishlist", done: "Added to Pocket Wishlist", path: "/api/v1/wishes",
			body: func(in sendIn) map[string]any { return map[string]any{"url": in.URL, "title": in.Title} },
			id:   f("id"), route: "/wishes/{id}"}},
	{slug: "rooms", name: "Pocket Rooms", bang: "rooms", reason: "Search your floor plans, rooms and measured furniture beside the web, and keep a piece of furniture you found to check whether it fits",
		search: &appSearch{scope: "rooms:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "results",
			id: f("id"), title: f("title"), snippet: f("subtitle"), routeOf: roomsRoute, kindOf: roomsKind, kind: "plan"},
		send: &appSend{scope: "rooms:write", label: "Will it fit? (Pocket Rooms)", done: "Added to your furniture in Pocket Rooms: put in its size", path: "/api/v1/pieces",
			body: func(in sendIn) map[string]any {
				return map[string]any{"name": in.Title, "url": in.URL, "status": "want", "notes": strings.TrimSpace(in.Snippet + "\n\nFound with Pocket Search — check its size on the shop page.")}
			}, id: f("id"), route: "/furniture/{id}"}},
	{slug: "puzzlecraft", name: "Pocket Puzzlecraft", bang: "puzzles", aliases: []string{"puzzle", "pz"}, reason: "Search your puzzles, escape rooms, puzzle books and word lists beside the web, and keep words you found as a word list to make puzzles from",
		search: &appSearch{scope: "puzzlecraft:read", path: "/api/v1/search", q: "q", list: "results",
			id: f("id"), title: f("title"), snippet: f("snippet"), date: f("date"), routeOf: puzzlecraftRoute, kindOf: puzzlecraftKind, kind: "puzzle"},
		send: &appSend{scope: "puzzlecraft:write", label: "Make a word list (Pocket Puzzlecraft)", done: "Added to your word lists in Pocket Puzzlecraft", path: "/api/v1/wordlists",
			body: func(in sendIn) map[string]any {
				return map[string]any{"name": in.Title, "text": strings.TrimSpace(in.Snippet)}
			}, id: f("id"), route: "/words?list={id}"}},
	{slug: "documents", name: "Pocket Documents", bang: "docs", reason: "Search your documents (titles and their text) beside the web",
		search: &appSearch{scope: "documents:read", path: "/api/v1/documents", q: "q", extra: url.Values{"limit": {"8"}}, list: "results",
			id: f("id"), title: f("title", "original_name"), snippet: f("content"), date: f("created"), route: "/documents/{id}", kind: "document"}},
	{slug: "files", name: "Pocket Files", bang: "files", reason: "Search your files (names and the text inside them) beside the web",
		search: &appSearch{scope: "files:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "hits",
			id: f("path"), title: f("name"), snippet: f("snippet", "folder"), date: f("modified"), route: "/f/{space}{path*}", kind: "file"}},
	{slug: "calendar", name: "Pocket Calendar", bang: "cal", reason: "Search your calendar events beside the web",
		search: &appSearch{scope: "calendar:read", path: "/api/v1/search", q: "q",
			id: f("event_id", "id"), title: f("title"), snippet: f("location", "description"), date: f("start"), route: "/event/{event_id}", kind: "event"}},
	{slug: "contacts", name: "Pocket Contacts", bang: "contacts", reason: "Search the people in your address book beside the web",
		search: &appSearch{scope: "contacts:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "contacts",
			id: f("id"), title: f("fn", "nickname"), snippet: f("org", "title"), route: "/contacts/{id}", kind: "contact"}},
	{slug: "projects", name: "Pocket Projects", bang: "issues", reason: "Search your project issues beside the web",
		search: &appSearch{scope: "projects:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "issues",
			id: f("identifier", "id"), title: f("title"), snippet: f("identifier"), date: f("updated_at"), route: "/issue/{identifier}", kind: "issue"}},
	{slug: "boards", name: "Pocket Boards", bang: "boards", reason: "Search the cards on your boards beside the web",
		search: &appSearch{scope: "boards:read", path: "/api/v1/search", q: "q", list: "cards",
			id: f("id"), title: f("title"), snippet: f("description"), date: f("due_at"), route: "/b/{board_id}/c/{id}", kind: "card"}},
	{slug: "bookshelf", name: "Pocket Bookshelf", bang: "books", reason: "Search the books on your shelf beside the web",
		search: &appSearch{scope: "books:read", path: "/api/v1/books", q: "q", extra: url.Values{"limit": {"8"}},
			id: f("id"), title: f("title"), snippet: f("subtitle", "publisher"), route: "/books/{id}", kind: "book"}},
	{slug: "library", name: "Pocket Library", bang: "lib", reason: "Search the books in your library beside the web",
		search: &appSearch{scope: "library:read", path: "/api/v1/books", q: "q", extra: url.Values{"limit": {"8"}}, list: "items",
			id: f("id"), title: f("title"), snippet: f("series", "subtitle", "publisher"), route: "/book/{id}", kind: "book"}},
	{slug: "inventory", name: "Pocket Inventory", bang: "inv", reason: "Search the things you own beside the web",
		search: &appSearch{scope: "items:read", path: "/api/v1/items", q: "q", extra: url.Values{"limit": {"8"}},
			id: f("id"), title: f("name"), snippet: f("brand", "model", "notes"), route: "/items/{id}", kind: "item"}},
	{slug: "mail", name: "Pocket Mail", bang: "mail", reason: "Search your email beside the web",
		search: &appSearch{scope: "mail:read", path: "/api/v1/messages", q: "q", extra: url.Values{"limit": {"8"}}, list: "messages",
			id: f("id"), title: f("subject"), snippet: f("snippet"), date: f("date"), route: "/message/{id}", kind: "email"}},
	{slug: "podcasts", name: "Pocket Podcasts", bang: "pod", reason: "Search podcast episodes you follow beside the web",
		search: &appSearch{scope: "history:read", path: "/api/v1/episodes", q: "q", extra: url.Values{"limit": {"8"}}, list: "episodes",
			id: f("id"), title: f("title"), snippet: f("description"), date: f("pub_date"), route: "/episodes/{id}", kind: "episode"}},
	{slug: "guitar", name: "Pocket Guitar", bang: "tabs", reason: "Search your guitar tabs beside the web",
		search: &appSearch{scope: "guitar:read", path: "/api/v1/tabs", q: "q", extra: url.Values{"limit": {"8"}}, list: "tabs",
			id: f("id"), title: f("title"), snippet: f("artist", "album"), route: "/tab/{id}", kind: "tab"}},
	{slug: "draw", name: "Pocket Draw", bang: "draw", reason: "Search your drawings beside the web",
		search: &appSearch{scope: "drawings:read", path: "/api/v1/drawings", q: "q", extra: url.Values{"limit": {"8"}}, list: "drawings",
			id: f("id"), title: f("title"), snippet: f("kind"), date: f("updated_at"), route: "/d/{id}", kind: "drawing"}},
	{slug: "blog", name: "Pocket Blog", bang: "blog", reason: "Search your blog posts (drafts too) beside the web",
		search: &appSearch{scope: "blog:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "results",
			id: f("id"), title: f("title"), snippet: f("snippet"), date: f("date"), route: "/posts/{id}", kind: "post"}},
	{slug: "chat", name: "Pocket Chat", bang: "chat", reason: "Search your chat messages beside the web",
		search: &appSearch{scope: "chat:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "results",
			id: f("id"), title: f("conversation_title", "author_name"), titleOf: chatTitle, snippet: f("snippet", "body"), date: f("created_at"),
			route: "/c/{conversation_id}?m={id}", kind: "message"}},
	{slug: "forms", name: "Pocket Forms", bang: "forms", reason: "Search your forms and the answers people sent beside the web",
		search: &appSearch{scope: "forms:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "items",
			id: f("id"), title: f("title"), snippet: f("subtitle"), date: f("date"), routeOf: formsRoute, kindOf: formsKind, kind: "form"}},
	{slug: "crm", name: "Pocket CRM", bang: "crm", reason: "Search your deals beside the web",
		search: &appSearch{scope: "crm:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "deals",
			id: f("id"), title: f("title"), snippet: f("company", "contact_name", "next_action"), snippetOf: crmLine, date: f("updated_at"),
			route: "/deals/{id}", kind: "deal"}},
	{slug: "league", name: "Pocket League", bang: "league", aliases: []string{"sport", "sports"}, reason: "Search your sport groups, games, players and the groups' chat beside the web",
		search: &appSearch{scope: "league:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "results",
			id: f("id"), title: f("title"), snippet: f("subtitle"), date: f("at"), routeOf: leagueRoute, kindOf: leagueKind, kind: "game"}},
	{slug: "tables", name: "Pocket Tables", bang: "tables", aliases: []string{"table"}, reason: "Search the rows and tables in your own small databases beside the web",
		search: &appSearch{scope: "tables:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, rowsOf: tablesRows,
			id: f("id"), title: f("title", "name"), snippetOf: tablesLine, date: f("updated_at"), routeOf: tablesRoute, kindOf: tablesKind, kind: "row"}},
	{slug: "rentals", name: "Pocket Rentals", bang: "rentals", aliases: []string{"rent"}, reason: "Search the homes you rent out — places, leases, tenants and repairs — beside the web",
		search: &appSearch{scope: "rentals:read", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"8"}}, list: "results",
			id: f("id"), title: f("title"), snippet: f("subtitle"), routeOf: rentalsRoute, kindOf: rentalsKind, kind: "lease"}},
	{slug: "trails", name: "Pocket Trails", bang: "trails", aliases: []string{"hike"}, reason: "Search your hikes, walks, runs and rides — routes, outings, gear and the spots along the way — beside the web",
		search: &appSearch{scope: "trails:read", path: "/api/v1/search", q: "q", list: "results",
			id: f("id"), title: f("title"), snippet: f("subtitle"), date: f("date"), routeOf: trailsRoute, kindOf: trailsKind, kind: "route"}},
	{slug: "travel", name: "Pocket Travel", bang: "trips", reason: "Search your trips beside the web",
		search: &appSearch{scope: "trips:read", path: "/api/v1/trips", q: "q",
			id: f("id"), title: f("name"), snippet: f("destination", "description"), date: f("start_date"), route: "/trips/{id}", kind: "trip"}},
	{slug: "music", name: "Pocket Music", bang: "music", reason: "Search albums in your music library beside the web (Pocket Music lets apps search its library with its playlists permission)",
		search: &appSearch{scope: "playlists:write", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"5"}}, list: "albums",
			id: f("id"), title: f("name"), snippet: f("artist", "year"), route: "/albums/{id}", kind: "album"}},
}

func f(names ...string) []string { return names }

// chatTitle: "Ana in Weekend plans" (who wrote it, in which conversation).
func chatTitle(row map[string]any) string {
	who, where := pick(row, f("author_name")), pick(row, f("conversation_title"))
	if who == "" || where == "" {
		return ""
	}
	return who + " in " + where
}

// formsRoute: an answer opens in the Forms inbox (/answers/{id}), a form in
// its editor (/forms/{id}).
func formsRoute(row map[string]any) string {
	if pick(row, f("kind")) == "response" {
		return "/answers/{id}"
	}
	return "/forms/{id}"
}

// formsKind: "answer" or "form" on the card.
func formsKind(row map[string]any) string {
	if pick(row, f("kind")) == "response" {
		return "answer"
	}
	return "form"
}

// leagueRoute: the row's own page in Pocket League ("/groups/1", "/games/31",
// "/groups/1/players/2", "/groups/1?tab=chat"), else its kind's page.
// roomsRoute is a Pocket Rooms result's page: its own path (a plan, a
// layout with a room on it, a piece of furniture).
func puzzlecraftRoute(row map[string]any) string {
	if p := pick(row, f("path")); strings.HasPrefix(p, "/") && !strings.ContainsAny(p, "{}") {
		return p
	}
	return "/"
}

func puzzlecraftKind(row map[string]any) string {
	switch pick(row, f("type")) {
	case "adventure":
		return "escape room"
	case "book":
		return "puzzle book"
	case "wordlist":
		return "word list"
	}
	return "puzzle"
}

func roomsRoute(row map[string]any) string {
	if p := pick(row, f("path")); strings.HasPrefix(p, "/") && !strings.ContainsAny(p, "{}") {
		return p
	}
	if pick(row, f("kind")) == "piece" {
		return "/furniture/{id}"
	}
	return "/"
}

func roomsKind(row map[string]any) string {
	switch pick(row, f("kind")) {
	case "room":
		return "room"
	case "piece":
		return "furniture"
	}
	return "plan"
}

func leagueRoute(row map[string]any) string {
	if p := pick(row, f("path")); strings.HasPrefix(p, "/") && !strings.ContainsAny(p, "{}") {
		return p
	}
	switch pick(row, f("kind")) {
	case "group":
		return "/groups/{id}"
	case "game":
		return "/games/{id}"
	}
	return "/"
}

// leagueKind: "group", "game", "player" or "message" on the card.
func leagueKind(row map[string]any) string {
	switch k := pick(row, f("kind")); k {
	case "group", "game", "player", "message":
		return k
	}
	return ""
}

// rentalsRoute: the row's own page in Pocket Rentals ("/properties/2",
// "/leases/3", "/tenants/5", "/repairs/9"), from its path or its kind.
func rentalsRoute(row map[string]any) string {
	if p := pick(row, f("path")); strings.HasPrefix(p, "/") && !strings.ContainsAny(p, "{}") {
		return p
	}
	switch pick(row, f("kind")) {
	case "property":
		return "/properties/{id}"
	case "lease":
		return "/leases/{id}"
	case "tenant":
		return "/tenants/{id}"
	case "repair":
		return "/repairs/{id}"
	}
	return "/"
}

// rentalsKind: "property", "lease", "tenant" or "repair" on the card.
func rentalsKind(row map[string]any) string {
	switch k := pick(row, f("kind")); k {
	case "property", "lease", "tenant", "repair":
		return k
	}
	return ""
}

// trailsRoute: the row's own page in Pocket Trails ("/routes/4",
// "/outings/12"; a spot opens its route, gear the gear page), from its path
// or its type.
func trailsRoute(row map[string]any) string {
	if p := pick(row, f("path")); strings.HasPrefix(p, "/") && !strings.ContainsAny(p, "{}") {
		return p
	}
	switch pick(row, f("type")) {
	case "route":
		return "/routes/{id}"
	case "outing":
		return "/outings/{id}"
	case "gear":
		return "/gear"
	}
	return "/"
}

// trailsKind: "route", "outing", "gear" or "spot" on the card.
func trailsKind(row map[string]any) string {
	switch k := pick(row, f("type")); k {
	case "route", "outing", "gear", "spot":
		return k
	}
	return ""
}

// tablesRows: the tables whose name matches first, then the rows (Pocket
// Tables answers {items: rows, tables}); a table is marked "_table".
func tablesRows(raw any) []map[string]any {
	var out []map[string]any
	for _, t := range rowsOf(raw, "tables") {
		t["_table"] = true
		out = append(out, t)
	}
	return append(out, rowsOf(raw, "items")...)
}

// tablesRoute: a row opens at /r/{id}, a table at /t/{id}.
func tablesRoute(row map[string]any) string {
	if row["_table"] == true {
		return "/t/{id}"
	}
	return "/r/{id}"
}

// tablesKind: "table" or "row" on the card.
func tablesKind(row map[string]any) string {
	if row["_table"] == true {
		return "table"
	}
	return "row"
}

// tablesLine: a row reads "Clients in CRM · Email: ana@pekara.ba" (its table,
// its base and the field that matched); a table "In CRM · 12 rows".
func tablesLine(row map[string]any) string {
	base := pick(row, f("base"))
	if row["_table"] == true {
		var parts []string
		if base != "" {
			parts = append(parts, "In "+base)
		}
		if n, ok := row["rows"].(float64); ok {
			if n == 1 {
				parts = append(parts, "1 row")
			} else {
				parts = append(parts, fmt.Sprintf("%.0f rows", n))
			}
		}
		return strings.Join(parts, " · ")
	}
	where := pick(row, f("table"))
	if where != "" && base != "" && base != where {
		where += " in " + base
	}
	match := pick(row, f("snippet"))
	if fl := pick(row, f("field")); fl != "" && match != "" {
		match = fl + ": " + match
	}
	var parts []string
	for _, p := range []string{where, match} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// crmLine: "Proposal sent · 4,500 EUR · Ana Marić (Pekara Marić)" — the
// deal's stage (or won/lost), its amount and who it is with.
func crmLine(row map[string]any) string {
	stage := pick(row, f("stage_name"))
	if st := pick(row, f("status")); st == "won" || st == "lost" {
		stage = strings.ToUpper(st[:1]) + st[1:]
	}
	money := ""
	if v, ok := row["amount"].(float64); ok && v != 0 {
		money = groupThousands(v)
		if c := pick(row, f("currency")); c != "" {
			money += " " + c
		}
	}
	who, co := pick(row, f("contact_name")), pick(row, f("company"))
	switch {
	case who != "" && co != "" && who != co:
		who += " (" + co + ")"
	case who == "":
		who = co
	}
	var parts []string
	for _, p := range []string{stage, money, who} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// groupThousands: 4500 → "4,500", 1234.5 → "1,234.50".
func groupThousands(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%.2f", v)
	whole, frac := s[:len(s)-3], s[len(s)-2:]
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if frac != "00" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// noteMarkdown is a result kept as a note.
func noteMarkdown(in sendIn) string {
	var b strings.Builder
	b.WriteString("[" + strings.NewReplacer("[", "(", "]", ")").Replace(in.Title) + "](" + in.URL + ")\n\n")
	if in.Snippet != "" {
		b.WriteString("> " + in.Snippet + "\n\n")
	}
	if in.Note != "" {
		b.WriteString(in.Note + "\n\n")
	}
	if in.Query != "" {
		b.WriteString("_Found searching for “" + in.Query + "” in Pocket Search._\n")
	}
	return b.String()
}
