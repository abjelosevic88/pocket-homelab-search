package server

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-search/internal/instant"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// ---- instant answers: worked out on the server, shown above the results ----
//
// Sums, conversions and the time in a city never leave the server; currency
// and weather ask keyless public APIs from the server (kept a while); "my
// tasks today", "next trip" and "my calendar today" read the person's own
// Pocket apps through their Core links, so nothing new to set up.

var (
	reTasksToday = regexp.MustCompile(`^\s*(?:my\s+)?(?:tasks?|todos?|to-?dos?)(?:\s+(?:for\s+)?today)?\s*\??\s*$|^\s*(?:today'?s?|what'?s?\s+due(?:\s+today)?)\s+tasks?\s*\??\s*$|^\s*what\s+(?:do\s+i\s+have\s+to|should\s+i)\s+do(?:\s+today)?\s*\??\s*$`)
	reNextTrip   = regexp.MustCompile(`^\s*(?:my\s+)?(?:next|upcoming)\s+(?:trip|travel|holiday|vacation)\s*\??\s*$|^\s*when\s+is\s+my\s+next\s+trip\s*\??\s*$`)
	reAgenda     = regexp.MustCompile(`^\s*(?:my\s+)?(?:calendar|agenda|schedule|events?)(?:\s+(today|tomorrow))?\s*\??\s*$|^\s*(?:what'?s|what\s+is)\s+on\s+(?:my\s+calendar\s+)?(today|tomorrow)\s*\??\s*$|^\s*(today|tomorrow)'?s?\s+(?:calendar|agenda|schedule|events)\s*\??\s*$`)
)

func (s *Server) instantAnswer(ctx context.Context, u *store.User, st store.Settings, q string) *instant.Answer {
	if len(q) > 120 {
		return nil
	}
	if a := s.appsAnswer(ctx, u, st, q); a != nil {
		return a
	}
	now := time.Now()
	if s.engineName() == "demo" {
		// no outside requests in the demo: the offline kinds only
		if a := instant.Convert(q); a != nil {
			return a
		}
		if a := instant.Calc(q); a != nil {
			return a
		}
		return instant.TimeIn(q, now)
	}
	return s.online.Answer(ctx, q, now)
}

func (s *Server) appsAnswer(ctx context.Context, u *store.User, st store.Settings, q string) *instant.Answer {
	lower := strings.ToLower(q)
	switch {
	case reTasksToday.MatchString(lower):
		return s.tasksAnswer(ctx, u, st)
	case reNextTrip.MatchString(lower):
		return s.tripAnswer(ctx, u, st)
	case reAgenda.MatchString(lower):
		m := reAgenda.FindStringSubmatch(lower)
		return s.agendaAnswer(ctx, u, st, strings.Contains(strings.Join(m[1:], " "), "tomorrow"))
	}
	return nil
}

func (s *Server) sibling(ctx context.Context, u *store.User, st store.Settings, slug string) (*siblingConn, pocketApp) {
	a, ok := appBySlug(slug)
	if !ok || a.search == nil {
		return nil, a
	}
	sib, err := s.siblingFor(ctx, u, st, a, a.search.scope)
	if err != nil {
		return nil, a
	}
	return sib, a
}

func (s *Server) tasksAnswer(ctx context.Context, u *store.User, st store.Settings) *instant.Answer {
	if s.demoApps() {
		return &instant.Answer{Kind: "tasks", Title: "Your tasks today", Value: "3 due today", Detail: "1 overdue · 7 this week", Source: "Pocket Tasks", URL: "#",
			Rows: []instant.Row{{Label: "Call the plumber", Value: "Today"}, {Label: "Renew car registration", Value: "Overdue"}, {Label: "Buy flour for the starter", Value: "Today"}}}
	}
	sib, _ := s.sibling(ctx, u, st, "tasks")
	if sib == nil {
		return nil
	}
	var d struct {
		Today        int `json:"today"`
		Overdue      int `json:"overdue"`
		UpcomingWeek int `json:"upcoming_week"`
		Next         []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Due   string `json:"due"`
		} `json:"next"`
	}
	if err := sib.client.Get(ctx, "/api/v1/dashboard", nil, &d); err != nil {
		return nil
	}
	a := &instant.Answer{Kind: "tasks", Title: "Your tasks today", Value: plural(d.Today, "task") + " due today",
		Detail: fmt.Sprintf("%d overdue · %d this week", d.Overdue, d.UpcomingWeek), Source: "Pocket Tasks", URL: strings.TrimRight(sib.public, "/") + "/"}
	for _, t := range d.Next[:min(len(d.Next), 6)] {
		a.Rows = append(a.Rows, instant.Row{Label: t.Title, Value: dueText(t.Due)})
	}
	return a
}

func (s *Server) tripAnswer(ctx context.Context, u *store.User, st store.Settings) *instant.Answer {
	if s.demoApps() {
		return &instant.Answer{Kind: "trip", Title: "Your next trip", Value: "Durmitor hiking weekend", Detail: "Žabljak, Montenegro · 14–16 Nov · in 13 days", Source: "Pocket Travel", URL: "#"}
	}
	sib, _ := s.sibling(ctx, u, st, "travel")
	if sib == nil {
		return nil
	}
	var trips []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Destination string `json:"destination"`
		Start       string `json:"start_date"`
		End         string `json:"end_date"`
	}
	if err := sib.client.Get(ctx, "/api/v1/trips", nil, &trips); err != nil {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	sort.Slice(trips, func(i, j int) bool { return trips[i].Start < trips[j].Start })
	for _, t := range trips {
		if len(t.Start) < 10 || (t.End != "" && t.End[:min(len(t.End), 10)] < today) || (t.End == "" && t.Start[:10] < today) {
			continue
		}
		start, _ := time.Parse("2006-01-02", t.Start[:10])
		when := start.Format("2 Jan")
		if len(t.End) >= 10 && t.End[:10] != t.Start[:10] {
			end, _ := time.Parse("2006-01-02", t.End[:10])
			when += " – " + end.Format("2 Jan")
		}
		days := int(time.Until(start).Hours()/24 + 0.99)
		rel := "under way"
		if t.Start[:10] > today {
			rel = "in " + plural(days, "day")
		}
		detail := strings.Trim(strings.Join([]string{t.Destination, when, rel}, " · "), " ·")
		return &instant.Answer{Kind: "trip", Title: "Your next trip", Value: t.Name, Detail: detail, Source: "Pocket Travel",
			URL: strings.TrimRight(sib.public, "/") + fmt.Sprintf("/trips/%d", t.ID)}
	}
	return &instant.Answer{Kind: "trip", Title: "Your next trip", Value: "No trip planned", Detail: "Nothing upcoming in Pocket Travel", Source: "Pocket Travel", URL: strings.TrimRight(sib.public, "/") + "/"}
}

func (s *Server) agendaAnswer(ctx context.Context, u *store.User, st store.Settings, tomorrow bool) *instant.Answer {
	label := "today"
	day := time.Now()
	if tomorrow {
		label, day = "tomorrow", day.AddDate(0, 0, 1)
	}
	if s.demoApps() {
		return &instant.Answer{Kind: "agenda", Title: "Your calendar " + label, Value: "3 events", Source: "Pocket Calendar", URL: "#",
			Rows: []instant.Row{{Label: "Standup", Value: "09:30"}, {Label: "Lunch with Mia", Value: "13:00"}, {Label: "Guitar practice", Value: "19:00"}}}
	}
	sib, _ := s.sibling(ctx, u, st, "calendar")
	if sib == nil {
		return nil
	}
	from := day.Format("2006-01-02")
	to := day.AddDate(0, 0, 1).Format("2006-01-02")
	var r struct {
		Events []struct {
			Title   string `json:"title"`
			Start   string `json:"start"`
			AllDay  bool   `json:"all_day"`
			EventID int64  `json:"event_id"`
		} `json:"events"`
	}
	if err := sib.client.Get(ctx, "/api/v1/events", url.Values{"from": {from}, "to": {to}, "tz": {time.Local.String()}}, &r); err != nil {
		return nil
	}
	a := &instant.Answer{Kind: "agenda", Title: "Your calendar " + label, Value: plural(len(r.Events), "event"), Source: "Pocket Calendar",
		URL: strings.TrimRight(sib.public, "/") + "/day/" + from}
	if len(r.Events) == 0 {
		a.Value, a.Detail = "Nothing planned", "Your calendar is free "+label
	}
	for _, e := range r.Events[:min(len(r.Events), 8)] {
		when := "All day"
		if !e.AllDay {
			if t, err := time.Parse(time.RFC3339, e.Start); err == nil {
				when = t.In(time.Local).Format("15:04")
			}
		}
		a.Rows = append(a.Rows, instant.Row{Label: e.Title, Value: when, URL: strings.TrimRight(sib.public, "/") + fmt.Sprintf("/event/%d", e.EventID)})
	}
	return a
}

// demoApps: the demo without Pocket Core makes up the apps' answers too.
func (s *Server) demoApps() bool {
	return s.cfg.DemoData && s.engineName() == "demo" && !s.core.Configured()
}

func dueText(due string) string {
	if len(due) < 10 {
		return ""
	}
	d := due[:10]
	today := time.Now().Format("2006-01-02")
	switch {
	case d < today:
		return "Overdue"
	case d == today:
		return "Today"
	case d == time.Now().AddDate(0, 0, 1).Format("2006-01-02"):
		return "Tomorrow"
	}
	if t, err := time.Parse("2006-01-02", d); err == nil {
		return t.Format("Mon 2 Jan")
	}
	return d
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
