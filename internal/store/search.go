package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Every query here is scoped by user_id: a row of somebody else's is
// ErrNotFound, never theirs to read or change.

// ---- history ----

// AddHistory remembers a search or an opened result.
func (s *Store) AddHistory(ctx context.Context, uid int64, h *HistoryEntry) error {
	if h.Kind == "" {
		h.Kind = KindSearch
	}
	h.CreatedAt = Now()
	// the same search again within a minute (paging, switching tabs back)
	// moves the old entry instead of adding another
	if h.Kind == KindSearch {
		res, err := s.DB.ExecContext(ctx, `UPDATE history SET created_at = ?, results = ?, category = ? WHERE user_id = ? AND kind = 'search' AND query = ? AND created_at > ?`,
			h.CreatedAt, h.Results, h.Category, uid, h.Query, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}
	}
	r, err := s.DB.ExecContext(ctx, `INSERT INTO history (user_id, kind, query, category, url, title, results, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, h.Kind, h.Query, h.Category, h.URL, h.Title, h.Results, h.CreatedAt)
	if err != nil {
		return err
	}
	h.ID, _ = r.LastInsertId()
	return nil
}

// HistoryFilter narrows a history listing.
type HistoryFilter struct {
	Q      string
	Kind   string
	Limit  int
	Offset int
}

// ListHistory lists a person's history, newest first.
func (s *Store) ListHistory(ctx context.Context, uid int64, f HistoryFilter) ([]HistoryEntry, int, error) {
	where := `user_id = ?`
	args := []any{uid}
	if f.Kind != "" {
		where += ` AND kind = ?`
		args = append(args, f.Kind)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		where += ` AND (query LIKE ? OR title LIKE ? OR url LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM history WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, kind, query, category, url, title, results, created_at FROM history WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, max(0, f.Offset))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		if err := rows.Scan(&h.ID, &h.Kind, &h.Query, &h.Category, &h.URL, &h.Title, &h.Results, &h.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// RecentQueries are a person's latest distinct searches (for the start page
// and suggestions), optionally starting with prefix.
func (s *Store) RecentQueries(ctx context.Context, uid int64, prefix string, limit int) ([]string, error) {
	q := `SELECT query FROM history WHERE user_id = ? AND kind = 'search' AND query != ''`
	args := []any{uid}
	if prefix = strings.TrimSpace(prefix); prefix != "" {
		q += ` AND query LIKE ? ESCAPE '\'`
		args = append(args, likeEscape(prefix)+"%")
	}
	q += ` GROUP BY query ORDER BY MAX(created_at) DESC LIMIT ?`
	rows, err := s.DB.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// DeleteHistory forgets one entry of a person's.
func (s *Store) DeleteHistory(ctx context.Context, uid, id int64) error {
	r, err := s.DB.ExecContext(ctx, `DELETE FROM history WHERE id = ? AND user_id = ?`, id, uid)
	return affected(r, err)
}

// ClearHistory forgets everything (since = "" ) or everything after since.
func (s *Store) ClearHistory(ctx context.Context, uid int64, since string) (int64, error) {
	r, err := s.DB.ExecContext(ctx, `DELETE FROM history WHERE user_id = ? AND created_at >= ?`, uid, since)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// PruneHistory drops a person's entries older than days.
func (s *Store) PruneHistory(ctx context.Context, uid int64, days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	cut := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
	r, err := s.DB.ExecContext(ctx, `DELETE FROM history WHERE user_id = ? AND created_at < ?`, uid, cut)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// ---- site rules ----

// NormDomain turns "https://www.Example.com/x" into "example.com".
func NormDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	if i := strings.LastIndex(d, "@"); i >= 0 {
		d = d[i+1:]
	}
	if h, _, ok := strings.Cut(d, ":"); ok {
		d = h
	}
	d = strings.TrimPrefix(strings.Trim(d, "."), "www.")
	return d
}

// ListRules lists a person's site rules.
func (s *Store) ListRules(ctx context.Context, uid int64) ([]SiteRule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, domain, action, note, hits, created_at, updated_at FROM site_rules WHERE user_id = ?
		ORDER BY CASE action WHEN 'pin' THEN 0 WHEN 'boost' THEN 1 WHEN 'lower' THEN 2 ELSE 3 END, domain`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SiteRule{}
	for rows.Next() {
		var r SiteRule
		if err := rows.Scan(&r.ID, &r.Domain, &r.Action, &r.Note, &r.Hits, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetRule adds a rule for a domain or changes the one there is.
func (s *Store) SetRule(ctx context.Context, uid int64, r *SiteRule) error {
	r.Domain = NormDomain(r.Domain)
	now := Now()
	err := s.DB.QueryRowContext(ctx, `INSERT INTO site_rules (user_id, domain, action, note, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, domain) DO UPDATE SET action = excluded.action, note = excluded.note, updated_at = excluded.updated_at
		RETURNING id, hits, created_at, updated_at`, uid, r.Domain, r.Action, r.Note, now, now).Scan(&r.ID, &r.Hits, &r.CreatedAt, &r.UpdatedAt)
	return err
}

// GetRule reads one of a person's rules.
func (s *Store) GetRule(ctx context.Context, uid, id int64) (*SiteRule, error) {
	var r SiteRule
	err := s.DB.QueryRowContext(ctx, `SELECT id, domain, action, note, hits, created_at, updated_at FROM site_rules WHERE id = ? AND user_id = ?`, id, uid).
		Scan(&r.ID, &r.Domain, &r.Action, &r.Note, &r.Hits, &r.CreatedAt, &r.UpdatedAt)
	return &r, notFound(err)
}

// UpdateRule changes one of a person's rules (domain, action, note).
func (s *Store) UpdateRule(ctx context.Context, uid int64, r *SiteRule) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE site_rules SET domain = ?, action = ?, note = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		NormDomain(r.Domain), r.Action, r.Note, Now(), r.ID, uid)
	return affected(res, conflict(err))
}

// DeleteRule removes one of a person's rules.
func (s *Store) DeleteRule(ctx context.Context, uid, id int64) error {
	r, err := s.DB.ExecContext(ctx, `DELETE FROM site_rules WHERE id = ? AND user_id = ?`, id, uid)
	return affected(r, err)
}

// CountRuleHits adds to the "changed N results" counters.
func (s *Store) CountRuleHits(ctx context.Context, uid int64, hits map[int64]int) {
	for id, n := range hits {
		_, _ = s.DB.ExecContext(ctx, `UPDATE site_rules SET hits = hits + ? WHERE id = ? AND user_id = ?`, n, id, uid)
	}
}

// ---- bangs ----

// ListBangs lists a person's own bangs.
func (s *Store) ListBangs(ctx context.Context, uid int64) ([]Bang, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, trigger, name, url, uses, created_at FROM bangs WHERE user_id = ? ORDER BY trigger`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bang{}
	for rows.Next() {
		var b Bang
		if err := rows.Scan(&b.ID, &b.Trigger, &b.Name, &b.URL, &b.Uses, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// FindBang finds a person's own bang by its trigger.
func (s *Store) FindBang(ctx context.Context, uid int64, trigger string) (*Bang, error) {
	var b Bang
	err := s.DB.QueryRowContext(ctx, `SELECT id, trigger, name, url, uses, created_at FROM bangs WHERE user_id = ? AND trigger = ?`, uid, strings.ToLower(trigger)).
		Scan(&b.ID, &b.Trigger, &b.Name, &b.URL, &b.Uses, &b.CreatedAt)
	return &b, notFound(err)
}

// CreateBang adds a bang (ErrConflict when the trigger is taken).
func (s *Store) CreateBang(ctx context.Context, uid int64, b *Bang) error {
	b.CreatedAt = Now()
	r, err := s.DB.ExecContext(ctx, `INSERT INTO bangs (user_id, trigger, name, url, created_at) VALUES (?, ?, ?, ?, ?)`, uid, b.Trigger, b.Name, b.URL, b.CreatedAt)
	if err != nil {
		return conflict(err)
	}
	b.ID, _ = r.LastInsertId()
	return nil
}

// UpdateBang changes one of a person's bangs.
func (s *Store) UpdateBang(ctx context.Context, uid int64, b *Bang) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE bangs SET trigger = ?, name = ?, url = ? WHERE id = ? AND user_id = ?`, b.Trigger, b.Name, b.URL, b.ID, uid)
	return affected(r, conflict(err))
}

// DeleteBang removes one of a person's bangs.
func (s *Store) DeleteBang(ctx context.Context, uid, id int64) error {
	r, err := s.DB.ExecContext(ctx, `DELETE FROM bangs WHERE id = ? AND user_id = ?`, id, uid)
	return affected(r, err)
}

// UsedBang counts one use (own bangs; built-in ones are counted in the cache).
func (s *Store) UsedBang(ctx context.Context, uid, id int64) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE bangs SET uses = uses + 1 WHERE id = ? AND user_id = ?`, id, uid)
}

// ---- saves ----

// AddSave remembers that a result went to an app.
func (s *Store) AddSave(ctx context.Context, uid int64, v *Save) error {
	v.CreatedAt = Now()
	return s.DB.QueryRowContext(ctx, `INSERT INTO saves (user_id, url, app, title, item_url, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, url, app) DO UPDATE SET title = excluded.title, item_url = excluded.item_url, created_at = excluded.created_at
		RETURNING id`, uid, v.URL, v.App, v.Title, v.ItemURL, v.CreatedAt).Scan(&v.ID)
}

// SavesFor lists where a person already sent these addresses: url → saves.
func (s *Store) SavesFor(ctx context.Context, uid int64, urls []string) (map[string][]Save, error) {
	out := map[string][]Save{}
	if len(urls) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(urls)), ",")
	args := []any{uid}
	for _, u := range urls {
		args = append(args, u)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, url, app, title, item_url, created_at FROM saves WHERE user_id = ? AND url IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v Save
		if err := rows.Scan(&v.ID, &v.URL, &v.App, &v.Title, &v.ItemURL, &v.CreatedAt); err != nil {
			return nil, err
		}
		out[v.URL] = append(out[v.URL], v)
	}
	return out, rows.Err()
}

// ListSaves lists a person's saves, newest first.
func (s *Store) ListSaves(ctx context.Context, uid int64, limit int) ([]Save, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, url, app, title, item_url, created_at FROM saves WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Save{}
	for rows.Next() {
		var v Save
		if err := rows.Scan(&v.ID, &v.URL, &v.App, &v.Title, &v.ItemURL, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func affected(r sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
