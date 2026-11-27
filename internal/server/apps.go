package server

import (
	"strings"
)

// pocketApps are the Pocket apps Search does more in than search: a result
// sent to them ("Save as a note", "Add as a task"…) or an instant answer read
// from them ("my tasks today"). Searching your Pocket apps needs no entry
// here: every app that says in its manifest that it can be searched is
// searched through Pocket Core's search capability, with its own bangs
// (capable.go). Every app is reached with only the scope that work needs;
// each link starts off in Pocket Core until the person (or an admin)
// switches it on.
//
// route: the new item's page in the app; {field} is filled from the answer.
var pocketApps = []pocketApp{
	{slug: "notes", name: "Pocket Notes", reason: "Keep a result as a note (title, link and snippet)",
		send: &appSend{scope: "notes:write", label: "Save as a note", done: "Saved to Pocket Notes", path: "/api/v1/notes",
			body: func(in sendIn) map[string]any {
				return map[string]any{"title": in.Title, "category": "Web", "content": noteMarkdown(in)}
			}, id: f("id"), route: "/note/{id}"}},
	{slug: "reader", name: "Pocket Reader", reason: "Save a result to read later",
		send: &appSend{scope: "reading:write", label: "Read later in Reader", done: "Saved to Pocket Reader", path: "/api/v1/saved",
			body: func(in sendIn) map[string]any {
				return map[string]any{"url": in.URL, "title": in.Title, "note": in.Note}
			},
			id: f("article.id"), route: "/a/{id}"}},
	{slug: "tasks", name: "Pocket Tasks", reason: "Turn a result into a task with its link, and show what is due today when you search “my tasks today”",
		answer: "tasks:read",
		send: &appSend{scope: "tasks:write", label: "Add as a task", done: "Added to Pocket Tasks", path: "/api/v1/tasks",
			body: func(in sendIn) map[string]any {
				return map[string]any{"title": in.Title, "url": in.URL, "notes": strings.TrimSpace(in.Snippet + "\n\n" + in.URL)}
			}, id: f("id")}},
	{slug: "lookout", name: "Pocket Lookout", reason: "Start watching a result for changes",
		send: &appSend{scope: "watches:write", label: "Watch for changes", done: "Pocket Lookout watches it now", path: "/api/v1/watches",
			body: func(in sendIn) map[string]any { return map[string]any{"url": in.URL, "title": in.Title} },
			id:   f("id"), route: "/watches/{id}"}},
	{slug: "wishlist", name: "Pocket Wishlist", reason: "Add a product you found to your wishlist (price read from the shop)",
		send: &appSend{scope: "wishes:write", label: "Add to wishlist", done: "Added to Pocket Wishlist", path: "/api/v1/wishes",
			body: func(in sendIn) map[string]any { return map[string]any{"url": in.URL, "title": in.Title} },
			id:   f("id"), route: "/wishes/{id}"}},
	{slug: "rooms", name: "Pocket Rooms", reason: "Keep a piece of furniture you found, to check whether it fits",
		send: &appSend{scope: "rooms:write", label: "Will it fit? (Pocket Rooms)", done: "Added to your furniture in Pocket Rooms: put in its size", path: "/api/v1/pieces",
			body: func(in sendIn) map[string]any {
				return map[string]any{"name": in.Title, "url": in.URL, "status": "want", "notes": strings.TrimSpace(in.Snippet + "\n\nFound with Pocket Search — check its size on the shop page.")}
			}, id: f("id"), route: "/furniture/{id}"}},
	{slug: "puzzlecraft", name: "Pocket Puzzlecraft", reason: "Keep words you found as a word list to make puzzles from",
		send: &appSend{scope: "puzzlecraft:write", label: "Make a word list (Pocket Puzzlecraft)", done: "Added to your word lists in Pocket Puzzlecraft", path: "/api/v1/wordlists",
			body: func(in sendIn) map[string]any {
				return map[string]any{"name": in.Title, "text": strings.TrimSpace(in.Snippet)}
			}, id: f("id"), route: "/words?list={id}"}},
	{slug: "calendar", name: "Pocket Calendar", reason: "Show your day when you search “my calendar today” (or tomorrow)",
		answer: "calendar:read"},
	{slug: "travel", name: "Pocket Travel", reason: "Show your next trip when you search “next trip”",
		answer: "trips:read"},
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
