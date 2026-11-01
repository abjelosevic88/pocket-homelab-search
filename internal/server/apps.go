package server

import (
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
	{slug: "documents", name: "Pocket Documents", bang: "docs", reason: "Search your documents (titles and their text) beside the web",
		search: &appSearch{scope: "documents:read", path: "/api/v1/documents", q: "q", extra: url.Values{"limit": {"8"}}, list: "results",
			id: f("id"), title: f("title", "original_name"), snippet: f("content"), date: f("created"), route: "/documents/{id}", kind: "document"}},
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
	{slug: "travel", name: "Pocket Travel", bang: "trips", reason: "Search your trips beside the web",
		search: &appSearch{scope: "trips:read", path: "/api/v1/trips", q: "q",
			id: f("id"), title: f("name"), snippet: f("destination", "description"), date: f("start_date"), route: "/trips/{id}", kind: "trip"}},
	{slug: "music", name: "Pocket Music", bang: "music", reason: "Search albums in your music library beside the web (Pocket Music lets apps search its library with its playlists permission)",
		search: &appSearch{scope: "playlists:write", path: "/api/v1/search", q: "q", extra: url.Values{"limit": {"5"}}, list: "albums",
			id: f("id"), title: f("name"), snippet: f("artist", "year"), route: "/albums/{id}", kind: "album"}},
}

func f(names ...string) []string { return names }

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
