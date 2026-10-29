package store

import (
	"context"
)

// DayStat is one study day's answers.
type DayStat struct {
	Day        string `json:"day"`
	Reviews    int    `json:"reviews"`     // every answer
	New        int    `json:"new"`         // first answers of new cards
	Learn      int    `json:"learn"`       // answers in a (re)learning step
	Review     int    `json:"review"`      // answers of cards in review
	Again      int    `json:"again"`       // "Again" on a card in review
	Mature     int    `json:"mature"`      // answers of mature cards in review
	MatureOK   int    `json:"mature_ok"`   // … not "Again"
	DurationMS int64  `json:"duration_ms"` // time spent
}

// DayStats returns the answers per study day between two days (inclusive), only days with answers.
func (s *Store) DayStats(ctx context.Context, uid int64, from, to string, deck int64) ([]DayStat, error) {
	q := `SELECT day, COUNT(1), COALESCE(SUM(state = 0), 0), COALESCE(SUM(state IN (1, 3)), 0), COALESCE(SUM(state = 2), 0),
		COALESCE(SUM(state = 2 AND rating = 1), 0), COALESCE(SUM(state = 2 AND last_interval >= 21), 0), COALESCE(SUM(state = 2 AND last_interval >= 21 AND rating > 1), 0),
		COALESCE(SUM(duration_ms), 0)
		FROM reviews WHERE user_id = ? AND day >= ? AND day <= ?`
	args := []any{uid, from, to}
	if deck > 0 {
		q += ` AND deck_id = ?`
		args = append(args, deck)
	}
	rows, err := s.DB.QueryContext(ctx, q+` GROUP BY day ORDER BY day`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayStat{}
	for rows.Next() {
		var d DayStat
		if err := rows.Scan(&d.Day, &d.Reviews, &d.New, &d.Learn, &d.Review, &d.Again, &d.Mature, &d.MatureOK, &d.DurationMS); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// StudyDays returns every study day with at least one answer, newest first (for streaks).
func (s *Store) StudyDays(ctx context.Context, uid int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT day FROM reviews WHERE user_id = ? ORDER BY day DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Totals are lifetime numbers.
type Totals struct {
	Reviews    int    `json:"reviews"`
	DurationMS int64  `json:"duration_ms"`
	Days       int    `json:"days"`
	Notes      int    `json:"notes"`
	Cards      int    `json:"cards"`
	First      string `json:"first_day"`
}

// LifetimeTotals sums everything the person did.
func (s *Store) LifetimeTotals(ctx context.Context, uid int64) (Totals, error) {
	var t Totals
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1), COALESCE(SUM(duration_ms), 0), COUNT(DISTINCT day), COALESCE(MIN(day), '') FROM reviews WHERE user_id = ?`, uid).Scan(&t.Reviews, &t.DurationMS, &t.Days, &t.First)
	if err != nil {
		return t, err
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM notes WHERE user_id = ?`, uid).Scan(&t.Notes)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM cards WHERE user_id = ?`, uid).Scan(&t.Cards)
	return t, nil
}

// HourStat is answers by hour of the day.
type HourStat struct {
	Hour    int `json:"hour"`
	Reviews int `json:"reviews"`
	OK      int `json:"ok"` // not "Again", of cards in review
	Total   int `json:"total"`
}

// RecentReviews returns the reviews since a moment (for hour-of-day and similar breakdowns).
func (s *Store) RecentReviews(ctx context.Context, uid int64, since string) ([]Review, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, card_id, deck_id, rating, state, elapsed_days, last_interval, interval, stability, difficulty, retrievability, duration_ms, reviewed_at, day FROM reviews WHERE user_id = ? AND reviewed_at >= ? ORDER BY id`, uid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Review{}
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.CardID, &r.DeckID, &r.Rating, &r.State, &r.ElapsedDays, &r.LastInterval, &r.Interval, &r.Stability, &r.Difficulty, &r.Retrievability, &r.DurationMS, &r.ReviewedAt, &r.Day); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CardReviews returns one card's answers, oldest first.
func (s *Store) CardReviews(ctx context.Context, uid, cardID int64) ([]Review, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, card_id, deck_id, rating, state, elapsed_days, last_interval, interval, stability, difficulty, retrievability, duration_ms, reviewed_at, day FROM reviews WHERE user_id = ? AND card_id = ? ORDER BY id`, uid, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Review{}
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.CardID, &r.DeckID, &r.Rating, &r.State, &r.ElapsedDays, &r.LastInterval, &r.Interval, &r.Stability, &r.Difficulty, &r.Retrievability, &r.DurationMS, &r.ReviewedAt, &r.Day); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllReviews returns every answer of the person (the export), oldest first.
func (s *Store) AllReviews(ctx context.Context, uid int64) ([]Review, error) {
	return s.RecentReviews(ctx, uid, "")
}

// ImportReview adds an answer from an import (no undo).
func (s *Store) ImportReview(ctx context.Context, uid int64, r *Review) error {
	return insertReview(ctx, s.DB, uid, r)
}

// AddedPerDay counts the notes added per day (UTC date of created_at) since a day.
func (s *Store) AddedPerDay(ctx context.Context, uid int64, since string) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT substr(created_at, 1, 10), COUNT(1) FROM notes WHERE user_id = ? AND created_at >= ? GROUP BY 1`, uid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out[d] = n
	}
	return out, rows.Err()
}
