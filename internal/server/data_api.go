package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// ---- export / import / dashboard ----

type exportDoc struct {
	App      string               `json:"app"`
	Version  string               `json:"version"`
	Exported string               `json:"exported_at"`
	Rules    []store.SiteRule     `json:"site_rules"`
	Bangs    []store.Bang         `json:"bangs"`
	History  []store.HistoryEntry `json:"history"`
	Saves    []store.Save         `json:"saves"`
	Lenses   []store.Lens         `json:"lenses"`
}

// handleExportJSON: everything of the caller's own, as one JSON file.
func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	doc := exportDoc{App: "pocket-search", Version: Version, Exported: time.Now().UTC().Format(time.RFC3339)}
	var err error
	if doc.Rules, err = s.store.ListRules(ctx, uid); err != nil {
		s.fail(w, err, "export")
		return
	}
	if doc.Bangs, err = s.store.ListBangs(ctx, uid); err != nil {
		s.fail(w, err, "export")
		return
	}
	if doc.History, _, err = s.store.ListHistory(ctx, uid, store.HistoryFilter{Limit: 500}); err != nil {
		s.fail(w, err, "export")
		return
	}
	// all of it, page by page
	for off := 500; ; off += 500 {
		more, _, err := s.store.ListHistory(ctx, uid, store.HistoryFilter{Limit: 500, Offset: off})
		if err != nil || len(more) == 0 {
			break
		}
		doc.History = append(doc.History, more...)
	}
	if doc.Saves, err = s.store.ListSaves(ctx, uid, 1000); err != nil {
		s.fail(w, err, "export")
		return
	}
	if doc.Lenses, err = s.store.ListLenses(ctx, uid); err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-search-`+time.Now().Format("2006-01-02")+`.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleImport takes a Pocket Search export (site rules and bangs are
// merged; history only when ?history=1). Importing twice changes nothing.
// A plain list of domains ("one per line", or "domain action") becomes rules.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	limit := s.cfg.MaxBodyBytes
	if limit <= 0 {
		limit = 16 << 20
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "that file is too big")
		return
	}
	out := map[string]int{"rules": 0, "bangs": 0, "history": 0, "lenses": 0, "skipped": 0}
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "{") {
		var doc exportDoc
		if err := json.Unmarshal(body, &doc); err != nil || doc.App != "pocket-search" {
			writeErr(w, http.StatusBadRequest, "that is not a Pocket Search export")
			return
		}
		for _, rule := range doc.Rules {
			rule := rule
			if validRule(&rule) != "" {
				out["skipped"]++
				continue
			}
			if err := s.store.SetRule(ctx, uid, &rule); err == nil {
				out["rules"]++
			}
		}
		for _, b := range doc.Bangs {
			b := b
			if validBang(&b) != "" {
				out["skipped"]++
				continue
			}
			if err := s.store.CreateBang(ctx, uid, &b); err != nil {
				if errors.Is(err, store.ErrConflict) {
					out["skipped"]++
				}
				continue
			}
			out["bangs"]++
		}
		if len(doc.Lenses) > 0 {
			s.ensureLenses(ctx, uid)
			have, _ := s.store.ListLenses(ctx, uid)
			names := map[string]bool{}
			for _, l := range have {
				names[strings.ToLower(l.Name)] = true
			}
			for _, l := range doc.Lenses {
				l := l
				if cleanLens(&l) != "" || names[strings.ToLower(l.Name)] {
					out["skipped"]++
					continue
				}
				if err := s.store.CreateLens(ctx, uid, &l); err == nil {
					names[strings.ToLower(l.Name)] = true
					out["lenses"]++
				}
			}
		}
		if queryBool(r, "history") {
			for _, h := range doc.History {
				var n int
				_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM history WHERE user_id = ? AND kind = ? AND query = ? AND url = ? AND created_at = ?`, uid, h.Kind, h.Query, h.URL, h.CreatedAt).Scan(&n)
				if n > 0 || (h.Kind != store.KindSearch && h.Kind != store.KindOpen) || h.CreatedAt == "" {
					out["skipped"]++
					continue
				}
				if _, err := s.store.DB.ExecContext(ctx, `INSERT INTO history (user_id, kind, query, category, url, title, results, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					uid, h.Kind, h.Query, h.Category, h.URL, h.Title, h.Results, h.CreatedAt); err == nil {
					out["history"]++
				}
			}
		}
		s.forgetResults(uid)
		writeJSON(w, http.StatusOK, out)
		return
	}
	// a list of sites: "example.com" (hidden) or "example.com boost"
	for _, line := range store.Lines(trimmed) {
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(strings.ReplaceAll(line, ",", " "))
		rule := store.SiteRule{Domain: f[0], Action: store.RuleBlock}
		if len(f) > 1 {
			rule.Action = map[string]string{"raise": store.RuleBoost, "up": store.RuleBoost, "down": store.RuleLower, "hide": store.RuleBlock}[strings.ToLower(f[1])]
			if rule.Action == "" {
				rule.Action = strings.ToLower(f[1])
			}
		}
		if validRule(&rule) != "" {
			out["skipped"]++
			continue
		}
		if err := s.store.SetRule(ctx, uid, &rule); err == nil {
			out["rules"]++
		}
	}
	s.forgetResults(uid)
	writeJSON(w, http.StatusOK, out)
}

// handleDashboard: a small summary for the start page and Homepage widgets.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	count := func(q string, args ...any) int {
		var n int
		_ = s.store.DB.QueryRowContext(ctx, q, args...).Scan(&n)
		return n
	}
	day := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	week := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339Nano)
	recent, _ := s.store.RecentQueries(ctx, uid, "", 8)
	writeJSON(w, http.StatusOK, map[string]any{
		"searches_today": count(`SELECT COUNT(1) FROM history WHERE user_id = ? AND kind = 'search' AND created_at >= ?`, uid, day),
		"searches_week":  count(`SELECT COUNT(1) FROM history WHERE user_id = ? AND kind = 'search' AND created_at >= ?`, uid, week),
		"rules":          count(`SELECT COUNT(1) FROM site_rules WHERE user_id = ?`, uid),
		"hidden_total":   count(`SELECT COALESCE(SUM(hits), 0) FROM site_rules WHERE user_id = ? AND action = 'block'`, uid),
		"bangs":          count(`SELECT COUNT(1) FROM bangs WHERE user_id = ?`, uid),
		"saves":          count(`SELECT COUNT(1) FROM saves WHERE user_id = ?`, uid),
		"recent":         recent,
		"engine":         s.engineName(),
		"engine_ready":   s.engine.Configured(),
	})
}

func (s *Server) handleListSaves(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListSaves(ctx, actorID(ctx), queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "saves")
		return
	}
	writeJSON(w, http.StatusOK, list)
}
