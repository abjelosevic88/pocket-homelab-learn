package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const cardCols = `id, user_id, note_id, deck_id, ord, state, step, stability, difficulty, due, last_review, interval, reps, lapses, position, suspended, buried_until, flag, created_at, updated_at`

func scanCard(row interface{ Scan(...any) error }) (*Card, error) {
	var c Card
	var susp int
	if err := row.Scan(&c.ID, &c.UserID, &c.NoteID, &c.DeckID, &c.Ord, &c.State, &c.Step, &c.Stability, &c.Difficulty, &c.Due, &c.LastReview, &c.Interval, &c.Reps, &c.Lapses, &c.Position, &susp, &c.BuriedUntil, &c.Flag, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	c.Suspended = susp == 1
	return &c, nil
}

func (s *Store) queryCards(ctx context.Context, q string, args ...any) ([]Card, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// cardsOf returns the cards of the notes (every note when ids is nil), by note.
func (s *Store) cardsOf(ctx context.Context, uid int64, ids []int64) (map[int64][]Card, error) {
	q := `SELECT ` + cardCols + ` FROM cards WHERE user_id = ?`
	args := []any{uid}
	if ids != nil {
		if len(ids) == 0 {
			return map[int64][]Card{}, nil
		}
		in, a := idList(ids)
		q += ` AND note_id IN ` + in
		args = append(args, a...)
	}
	list, err := s.queryCards(ctx, q+` ORDER BY note_id, ord`, args...)
	if err != nil {
		return nil, err
	}
	out := map[int64][]Card{}
	for _, c := range list {
		out[c.NoteID] = append(out[c.NoteID], c)
	}
	return out, nil
}

// GetCard returns one of the person's cards.
func (s *Store) GetCard(ctx context.Context, uid, id int64) (*Card, error) {
	return scanCard(s.DB.QueryRowContext(ctx, `SELECT `+cardCols+` FROM cards WHERE id = ? AND user_id = ?`, id, uid))
}

func saveCard(ctx context.Context, ex execer, uid int64, c *Card) error {
	c.UpdatedAt = Now()
	res, err := ex.ExecContext(ctx, `UPDATE cards SET state = ?, step = ?, stability = ?, difficulty = ?, due = ?, last_review = ?, interval = ?, reps = ?, lapses = ?, suspended = ?, buried_until = ?, flag = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		c.State, c.Step, c.Stability, c.Difficulty, c.Due, c.LastReview, c.Interval, c.Reps, c.Lapses, boolInt(c.Suspended), c.BuriedUntil, c.Flag, c.UpdatedAt, c.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveCard writes a card's state, suspension, burial and flag.
func (s *Store) SaveCard(ctx context.Context, uid int64, c *Card) error {
	return saveCard(ctx, s.DB, uid, c)
}

// Scope is where the study queue looks: some decks, at some moment.
type Scope struct {
	Decks      []int64
	Today      string // study day
	LearnAhead string // learning cards due before this
	DayEnd     string // reviews due before this
}

func (sc Scope) base() (string, []any) {
	in, args := idList(sc.Decks)
	return `user_id = ? AND deck_id IN ` + in + ` AND suspended = 0 AND buried_until <= ?`, args
}

// NextLearning is the (re)learning card due first, before LearnAhead.
func (s *Store) NextLearning(ctx context.Context, uid int64, sc Scope, skip []int64) (*Card, error) {
	cond, args := sc.base()
	q := `SELECT ` + cardCols + ` FROM cards WHERE ` + cond + ` AND state IN (1, 3) AND due <= ?` + notIn(skip) + ` ORDER BY due, id LIMIT 1`
	return scanCard(s.DB.QueryRowContext(ctx, q, append(append(append([]any{uid}, args...), sc.Today, sc.LearnAhead), int64s(skip)...)...))
}

// NextLearningLater is the (re)learning card due next, however far ahead.
func (s *Store) NextLearningLater(ctx context.Context, uid int64, sc Scope) (*Card, error) {
	cond, args := sc.base()
	q := `SELECT ` + cardCols + ` FROM cards WHERE ` + cond + ` AND state IN (1, 3) ORDER BY due, id LIMIT 1`
	return scanCard(s.DB.QueryRowContext(ctx, q, append(append([]any{uid}, args...), sc.Today)...))
}

// NextReview is the review card due first today, in one of the decks.
func (s *Store) NextReview(ctx context.Context, uid int64, sc Scope, decks []int64, skip []int64) (*Card, error) {
	if len(decks) == 0 {
		return nil, ErrNotFound
	}
	sc.Decks = decks
	cond, args := sc.base()
	q := `SELECT ` + cardCols + ` FROM cards WHERE ` + cond + ` AND state = 2 AND due < ?` + notIn(skip) + ` ORDER BY due, id LIMIT 1`
	return scanCard(s.DB.QueryRowContext(ctx, q, append(append(append([]any{uid}, args...), sc.Today, sc.DayEnd), int64s(skip)...)...))
}

// NextNew is the next new card of a deck, in the deck's order.
func (s *Store) NextNew(ctx context.Context, uid int64, sc Scope, deck int64, random bool, skip []int64) (*Card, error) {
	sc.Decks = []int64{deck}
	cond, args := sc.base()
	order := `position, id`
	if random {
		order = `((id * 1103515245 + 12345) % 2147483647), id`
	}
	q := `SELECT ` + cardCols + ` FROM cards WHERE ` + cond + ` AND state = 0` + notIn(skip) + ` ORDER BY ` + order + ` LIMIT 1`
	return scanCard(s.DB.QueryRowContext(ctx, q, append(append(append([]any{uid}, args...), sc.Today), int64s(skip)...)...))
}

func notIn(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	return ` AND id NOT IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `)`
}

func int64s(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// BurySiblings sets aside the note's other new and review cards until the
// next study day, so the reverse of a card is not shown right after it.
func (s *Store) BurySiblings(ctx context.Context, uid int64, c *Card, until string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE cards SET buried_until = ?, updated_at = ? WHERE user_id = ? AND note_id = ? AND id <> ? AND state IN (0, 2) AND suspended = 0`, until, Now(), uid, c.NoteID, c.ID)
	return err
}

// SuspendCards sets cards aside (or brings them back).
func (s *Store) SuspendCards(ctx context.Context, uid int64, ids []int64, on bool) (int, error) {
	in, args := idList(ids)
	res, err := s.DB.ExecContext(ctx, `UPDATE cards SET suspended = ?, updated_at = ? WHERE user_id = ? AND id IN `+in, append([]any{boolInt(on), Now(), uid}, args...)...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CardIDsOfNotes returns the cards of notes.
func (s *Store) CardIDsOfNotes(ctx context.Context, uid int64, notes []int64) ([]int64, error) {
	in, args := idList(notes)
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM cards WHERE user_id = ? AND note_id IN `+in, append([]any{uid}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ForgetCards makes cards new again (their history stays in the statistics).
func (s *Store) ForgetCards(ctx context.Context, uid int64, ids []int64) (int, error) {
	in, args := idList(ids)
	res, err := s.DB.ExecContext(ctx, `UPDATE cards SET state = 0, step = 0, stability = 0, difficulty = 0, due = '', last_review = '', interval = 0, reps = 0, lapses = 0, buried_until = '', updated_at = ? WHERE user_id = ? AND id IN `+in, append([]any{Now(), uid}, args...)...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Answer saves a card after an answer and the review, together.
func (s *Store) Answer(ctx context.Context, uid int64, c *Card, r *Review) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveCard(ctx, tx, uid, c); err != nil {
		return err
	}
	if err := insertReview(ctx, tx, uid, r); err != nil {
		return err
	}
	return tx.Commit()
}

func insertReview(ctx context.Context, ex execer, uid int64, r *Review) error {
	res, err := ex.ExecContext(ctx, `INSERT INTO reviews (user_id, card_id, deck_id, rating, state, elapsed_days, last_interval, interval, stability, difficulty, retrievability, duration_ms, reviewed_at, day, prev)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, r.CardID, r.DeckID, r.Rating, r.State, r.ElapsedDays, r.LastInterval, r.Interval, r.Stability, r.Difficulty, r.Retrievability, r.DurationMS, r.ReviewedAt, r.Day, r.Prev)
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	r.UserID = uid
	return nil
}

// ErrNotLast means a later answer exists, so this one cannot be undone.
var ErrNotLast = fmt.Errorf("only the last answer of a card can be undone")

// Undo takes an answer back: the card returns to how it was before.
func (s *Store) Undo(ctx context.Context, uid, reviewID int64) (*Card, error) {
	var cardID, last int64
	var prev string
	if err := s.DB.QueryRowContext(ctx, `SELECT card_id, prev FROM reviews WHERE id = ? AND user_id = ?`, reviewID, uid).Scan(&cardID, &prev); err != nil {
		return nil, notFound(err)
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(id) FROM reviews WHERE card_id = ? AND user_id = ?`, cardID, uid).Scan(&last)
	if last != reviewID || prev == "" {
		return nil, ErrNotLast
	}
	var c Card
	if err := json.Unmarshal([]byte(prev), &c); err != nil {
		return nil, err
	}
	c.ID = cardID
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveCard(ctx, tx, uid, &c); err != nil {
		return nil, err
	}
	// the siblings this answer buried come back
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET buried_until = '' WHERE user_id = ? AND note_id = (SELECT note_id FROM cards WHERE id = ?) AND id <> ? AND buried_until > ?`, uid, cardID, cardID, c.BuriedUntil); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM reviews WHERE id = ? AND user_id = ?`, reviewID, uid); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetCard(ctx, uid, cardID)
}

// CardBrief is a card with what the study screen shows of its note.
type CardBrief struct {
	Card
	Kind  string `json:"kind"`
	Front string `json:"front"`
	Back  string `json:"back"`
	Extra string `json:"extra"`
	Tags  string `json:"tags"`
	Deck  string `json:"deck"`
	Src   string `json:"source"`
	Ref   string `json:"source_ref"`
	URL   string `json:"source_url"`
}

// Brief loads a card with its note's text.
func (s *Store) Brief(ctx context.Context, uid, cardID int64) (*CardBrief, error) {
	c, err := s.GetCard(ctx, uid, cardID)
	if err != nil {
		return nil, err
	}
	b := &CardBrief{Card: *c}
	err = s.DB.QueryRowContext(ctx, `SELECT n.kind, n.front, n.back, n.extra, n.tags, n.source, n.source_ref, n.source_url, d.name FROM notes n JOIN decks d ON d.id = n.deck_id WHERE n.id = ? AND n.user_id = ?`, c.NoteID, uid).
		Scan(&b.Kind, &b.Front, &b.Back, &b.Extra, &b.Tags, &b.Src, &b.Ref, &b.URL, &b.Deck)
	if err != nil {
		return nil, notFound(err)
	}
	return b, nil
}

// DueTimes returns when the person's review cards are due (not suspended), for the forecast.
func (s *Store) DueTimes(ctx context.Context, uid int64, until string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT due FROM cards WHERE user_id = ? AND suspended = 0 AND state IN (1, 2, 3) AND due < ? AND deck_id IN (SELECT id FROM decks WHERE user_id = ? AND archived = 0)`, uid, until, uid)
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

// IntervalCounts returns review cards by interval (days), for the statistics.
func (s *Store) IntervalCounts(ctx context.Context, uid int64) (map[int]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT interval, COUNT(1) FROM cards WHERE user_id = ? AND state = 2 AND suspended = 0 GROUP BY interval`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]int{}
	for rows.Next() {
		var i, n int
		if err := rows.Scan(&i, &n); err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, rows.Err()
}

// HardCards returns the cards forgotten most often (lapses ≥ min).
func (s *Store) HardCards(ctx context.Context, uid int64, min, limit int) ([]CardBrief, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id FROM cards c WHERE c.user_id = ? AND c.lapses >= ? AND c.suspended = 0 ORDER BY c.lapses DESC, c.difficulty DESC LIMIT ?`, uid, min, limit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []CardBrief{}
	for _, id := range ids {
		if b, err := s.Brief(ctx, uid, id); err == nil {
			out = append(out, *b)
		}
	}
	return out, nil
}
