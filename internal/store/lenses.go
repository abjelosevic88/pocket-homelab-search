package store

import (
	"context"
	"strings"
)

// Lens is a saved way of searching (see migration 0002).
type Lens struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Sites     string `json:"sites"`
	Boost     string `json:"boost"`
	Hide      string `json:"hide"`
	Words     string `json:"words"`
	Language  string `json:"language"`
	Position  int    `json:"position"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Domains is a lens list as clean domains.
func Domains(list string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range strings.FieldsFunc(list, func(r rune) bool { return r == '\n' || r == ',' || r == ' ' }) {
		d := NormDomain(l)
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

const lensCols = `id, name, icon, sites, boost, hide, words, language, position, created_at, updated_at`

func scanLens(sc interface{ Scan(...any) error }) (*Lens, error) {
	var l Lens
	err := sc.Scan(&l.ID, &l.Name, &l.Icon, &l.Sites, &l.Boost, &l.Hide, &l.Words, &l.Language, &l.Position, &l.CreatedAt, &l.UpdatedAt)
	return &l, err
}

// ListLenses lists a person's lenses in their order.
func (s *Store) ListLenses(ctx context.Context, uid int64) ([]Lens, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+lensCols+` FROM lenses WHERE user_id = ? ORDER BY position, id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Lens{}
	for rows.Next() {
		l, err := scanLens(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// GetLens reads one of a person's lenses.
func (s *Store) GetLens(ctx context.Context, uid, id int64) (*Lens, error) {
	l, err := scanLens(s.DB.QueryRowContext(ctx, `SELECT `+lensCols+` FROM lenses WHERE id = ? AND user_id = ?`, id, uid))
	return l, notFound(err)
}

// CreateLens adds a lens at the end.
func (s *Store) CreateLens(ctx context.Context, uid int64, l *Lens) error {
	now := Now()
	r, err := s.DB.ExecContext(ctx, `INSERT INTO lenses (user_id, name, icon, sites, boost, hide, words, language, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM lenses WHERE user_id = ?), ?, ?)`,
		uid, l.Name, l.Icon, l.Sites, l.Boost, l.Hide, l.Words, l.Language, uid, now, now)
	if err != nil {
		return err
	}
	l.ID, _ = r.LastInsertId()
	got, err := s.GetLens(ctx, uid, l.ID)
	if err == nil {
		*l = *got
	}
	return err
}

// UpdateLens changes one of a person's lenses.
func (s *Store) UpdateLens(ctx context.Context, uid int64, l *Lens) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE lenses SET name = ?, icon = ?, sites = ?, boost = ?, hide = ?, words = ?, language = ?, position = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, l.Name, l.Icon, l.Sites, l.Boost, l.Hide, l.Words, l.Language, l.Position, Now(), l.ID, uid)
	return affected(r, err)
}

// DeleteLens removes one of a person's lenses.
func (s *Store) DeleteLens(ctx context.Context, uid, id int64) error {
	r, err := s.DB.ExecContext(ctx, `DELETE FROM lenses WHERE id = ? AND user_id = ?`, id, uid)
	return affected(r, err)
}
