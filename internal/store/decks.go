package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// TS is the stored form of a moment: UTC, fixed width, so text comparisons order correctly.
func TS(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// ParseTS reads a stored moment (zero for "").
func ParseTS(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

const deckCols = `id, user_id, name, description, color, new_per_day, reviews_per_day, retention, new_order, archived, sort, source, external_id, created_at, updated_at`

func scanDeck(row interface{ Scan(...any) error }) (*Deck, error) {
	var d Deck
	var arch int
	if err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &d.Color, &d.NewPerDay, &d.ReviewsPerDay, &d.Retention, &d.NewOrder, &arch, &d.Sort, &d.Source, &d.ExternalID, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	d.Archived = arch == 1
	return &d, nil
}

// ListDecks returns a person's decks in their order (archived ones last, only when asked for).
func (s *Store) ListDecks(ctx context.Context, uid int64, archived bool) ([]Deck, error) {
	q := `SELECT ` + deckCols + ` FROM decks WHERE user_id = ?`
	if !archived {
		q += ` AND archived = 0`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY archived, sort, name COLLATE NOCASE`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Deck{}
	for rows.Next() {
		d, err := scanDeck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// GetDeck returns one of the person's decks.
func (s *Store) GetDeck(ctx context.Context, uid, id int64) (*Deck, error) {
	return scanDeck(s.DB.QueryRowContext(ctx, `SELECT `+deckCols+` FROM decks WHERE id = ? AND user_id = ?`, id, uid))
}

// GetDeckByName finds a deck by its name (case-insensitive).
func (s *Store) GetDeckByName(ctx context.Context, uid int64, name string) (*Deck, error) {
	return scanDeck(s.DB.QueryRowContext(ctx, `SELECT `+deckCols+` FROM decks WHERE user_id = ? AND name = ? COLLATE NOCASE`, uid, strings.TrimSpace(name)))
}

// CreateDeck adds a deck at the end of the list.
func (s *Store) CreateDeck(ctx context.Context, uid int64, d *Deck) error {
	if d.NewOrder == "" {
		d.NewOrder = "added"
	}
	if d.Source == "" {
		d.Source = "app"
	}
	if d.Sort == 0 {
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort), 0) + 1 FROM decks WHERE user_id = ?`, uid).Scan(&d.Sort)
	}
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO decks (user_id, name, description, color, new_per_day, reviews_per_day, retention, new_order, archived, sort, source, external_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, d.Name, d.Description, d.Color, d.NewPerDay, d.ReviewsPerDay, d.Retention, d.NewOrder, boolInt(d.Archived), d.Sort, d.Source, d.ExternalID, now, now)
	if err != nil {
		return conflict(err)
	}
	d.ID, _ = res.LastInsertId()
	d.UserID, d.CreatedAt, d.UpdatedAt = uid, now, now
	return nil
}

// UpdateDeck saves a deck's name and options.
func (s *Store) UpdateDeck(ctx context.Context, uid int64, d *Deck) error {
	d.UpdatedAt = Now()
	res, err := s.DB.ExecContext(ctx, `UPDATE decks SET name = ?, description = ?, color = ?, new_per_day = ?, reviews_per_day = ?, retention = ?, new_order = ?, archived = ?, sort = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		d.Name, d.Description, d.Color, d.NewPerDay, d.ReviewsPerDay, d.Retention, d.NewOrder, boolInt(d.Archived), d.Sort, d.UpdatedAt, d.ID, uid)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDeck removes a deck with all its notes, cards and reviews.
func (s *Store) DeleteDeck(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM decks WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderDecks sets the order of the given decks.
func (s *Store) ReorderDecks(ctx context.Context, uid int64, ids []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE decks SET sort = ? WHERE id = ? AND user_id = ?`, i+1, id, uid); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// CountWindow is "now" as the study queue sees it.
type CountWindow struct {
	Now         string // RFC3339 UTC
	LearnAhead  string // learning cards due before this are shown
	DayEnd      string // reviews due before this belong to today
	TomorrowEnd string
	Today       string // the study day, YYYY-MM-DD
}

// DeckCounts fills each deck's numbers for today, the daily limits applied.
func (s *Store) DeckCounts(ctx context.Context, uid int64, decks []Deck, w CountWindow) error {
	if len(decks) == 0 {
		return nil
	}
	byID := map[int64]*Deck{}
	for i := range decks {
		decks[i].Counts = &DeckCounts{}
		byID[decks[i].ID] = &decks[i]
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT deck_id, COUNT(1),
		COALESCE(SUM(state = 0 AND suspended = 0), 0),
		COALESCE(SUM(state IN (1, 3) AND suspended = 0), 0),
		COALESCE(SUM(state = 2 AND interval < 21 AND suspended = 0), 0),
		COALESCE(SUM(state = 2 AND interval >= 21 AND suspended = 0), 0),
		COALESCE(SUM(suspended = 1), 0),
		COALESCE(SUM(state IN (1, 3) AND suspended = 0 AND buried_until <= ? AND due <= ?), 0),
		COALESCE(SUM(state = 2 AND suspended = 0 AND buried_until <= ? AND due < ?), 0),
		COALESCE(SUM(state = 0 AND suspended = 0 AND buried_until <= ?), 0),
		COALESCE(SUM(state = 2 AND suspended = 0 AND due >= ? AND due < ?), 0)
		FROM cards WHERE user_id = ? GROUP BY deck_id`, w.Today, w.LearnAhead, w.Today, w.DayEnd, w.Today, w.DayEnd, w.TomorrowEnd, uid)
	if err != nil {
		return err
	}
	type raw struct{ learn, review, fresh int }
	raws := map[int64]raw{}
	for rows.Next() {
		var id int64
		var c DeckCounts
		var r raw
		if err := rows.Scan(&id, &c.Total, &c.New, &c.Learning, &c.Young, &c.Mature, &c.Suspended, &r.learn, &r.review, &r.fresh, &c.DueTomorow); err != nil {
			rows.Close()
			return err
		}
		if d := byID[id]; d != nil {
			*d.Counts = c
			raws[id] = r
		}
	}
	rows.Close()
	today, err := s.DayDeckCounts(ctx, uid, w.Today)
	if err != nil {
		return err
	}
	for id, d := range byID {
		r, t := raws[id], today[id]
		c := d.Counts
		c.DoneToday, c.NewToday = t.All, t.New
		c.DueLearn = r.learn
		c.DueNew = clampMin(r.fresh, d.NewPerDay-t.New)
		c.DueReview = clampMin(r.review, d.ReviewsPerDay-t.Review)
		if d.Archived {
			c.DueNew, c.DueLearn, c.DueReview = 0, 0, 0
		}
	}
	return nil
}

// clampMin is min(n, limit) with a negative limit counting as 0.
func clampMin(n, limit int) int {
	if limit < 0 {
		limit = 0
	}
	if n < limit {
		return n
	}
	return limit
}

// DayCount is how much was answered on one study day.
type DayCount struct{ All, New, Review int }

// DayDeckCounts returns the answers of one study day per deck.
func (s *Store) DayDeckCounts(ctx context.Context, uid int64, day string) (map[int64]DayCount, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT deck_id, COUNT(1), COALESCE(SUM(state = 0), 0), COALESCE(SUM(state = 2), 0) FROM reviews WHERE user_id = ? AND day = ? GROUP BY deck_id`, uid, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]DayCount{}
	for rows.Next() {
		var id int64
		var c DayCount
		if err := rows.Scan(&id, &c.All, &c.New, &c.Review); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// idList renders ids for an IN (…) clause with their arguments.
func idList(ids []int64) (string, []any) {
	if len(ids) == 0 {
		return "(NULL)", nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")", args
}

func nullStr(s sql.NullString) string { return s.String }
