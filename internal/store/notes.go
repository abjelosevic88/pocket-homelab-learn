package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const noteCols = `n.id, n.user_id, n.deck_id, n.kind, n.front, n.back, n.extra, n.tags, n.source, n.source_ref, n.source_url, n.external_id, n.created_at, n.updated_at`

func scanNote(row interface{ Scan(...any) error }, extra ...any) (*Note, error) {
	var n Note
	dst := []any{&n.ID, &n.UserID, &n.DeckID, &n.Kind, &n.Front, &n.Back, &n.Extra, &n.Tags, &n.Source, &n.SourceRef, &n.SourceURL, &n.ExternalID, &n.CreatedAt, &n.UpdatedAt}
	if err := row.Scan(append(dst, extra...)...); err != nil {
		return nil, notFound(err)
	}
	return &n, nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// nextPosition is where the next new card goes in the person's new-card order.
func nextPosition(ctx context.Context, ex execer, uid int64) int {
	var p int
	_ = ex.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM cards WHERE user_id = ?`, uid).Scan(&p)
	return p
}

// CreateNote adds a note and one new card per ord, in one transaction.
func (s *Store) CreateNote(ctx context.Context, uid int64, n *Note, ords []int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := s.createNoteTx(ctx, tx, uid, n, ords); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) createNoteTx(ctx context.Context, tx *sql.Tx, uid int64, n *Note, ords []int) error {
	now := Now()
	if n.Source == "" {
		n.Source = "app"
	}
	if n.CreatedAt == "" {
		n.CreatedAt = now
	}
	n.Tags = JoinTags(n.Tags)
	res, err := tx.ExecContext(ctx, `INSERT INTO notes (user_id, deck_id, kind, front, back, extra, tags, source, source_ref, source_url, external_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, n.DeckID, n.Kind, n.Front, n.Back, n.Extra, n.Tags, n.Source, n.SourceRef, n.SourceURL, n.ExternalID, n.CreatedAt, now)
	if err != nil {
		return conflict(err)
	}
	n.ID, _ = res.LastInsertId()
	n.UserID, n.UpdatedAt = uid, now
	pos := nextPosition(ctx, tx, uid)
	n.Cards = nil
	for i, ord := range ords {
		c := Card{NoteID: n.ID, DeckID: n.DeckID, Ord: ord, Position: pos + i}
		if err := insertCard(ctx, tx, uid, &c); err != nil {
			return err
		}
		n.Cards = append(n.Cards, c)
	}
	return nil
}

func insertCard(ctx context.Context, ex execer, uid int64, c *Card) error {
	now := Now()
	res, err := ex.ExecContext(ctx, `INSERT INTO cards (user_id, note_id, deck_id, ord, state, step, stability, difficulty, due, last_review, interval, reps, lapses, position, suspended, buried_until, flag, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, c.NoteID, c.DeckID, c.Ord, c.State, c.Step, c.Stability, c.Difficulty, c.Due, c.LastReview, c.Interval, c.Reps, c.Lapses, c.Position, boolInt(c.Suspended), c.BuriedUntil, c.Flag, now, now)
	if err != nil {
		return conflict(err)
	}
	c.ID, _ = res.LastInsertId()
	c.UserID, c.CreatedAt, c.UpdatedAt = uid, now, now
	return nil
}

// UpdateNote saves a note and brings its cards in line with ords: cards of
// ords that are gone are deleted (with their history), new ords get new
// cards, the others keep their memory state.
func (s *Store) UpdateNote(ctx context.Context, uid int64, n *Note, ords []int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	n.UpdatedAt = Now()
	n.Tags = JoinTags(n.Tags)
	res, err := tx.ExecContext(ctx, `UPDATE notes SET deck_id = ?, kind = ?, front = ?, back = ?, extra = ?, tags = ?, source_ref = ?, source_url = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		n.DeckID, n.Kind, n.Front, n.Back, n.Extra, n.Tags, n.SourceRef, n.SourceURL, n.UpdatedAt, n.ID, uid)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET deck_id = ?, updated_at = ? WHERE note_id = ? AND user_id = ? AND deck_id <> ?`, n.DeckID, n.UpdatedAt, n.ID, uid, n.DeckID); err != nil {
		return err
	}
	have := map[int]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT ord FROM cards WHERE note_id = ? AND user_id = ?`, n.ID, uid)
	if err != nil {
		return err
	}
	for rows.Next() {
		var o int
		if err := rows.Scan(&o); err != nil {
			rows.Close()
			return err
		}
		have[o] = true
	}
	rows.Close()
	want := map[int]bool{}
	pos := nextPosition(ctx, tx, uid)
	for _, o := range ords {
		want[o] = true
		if !have[o] {
			c := Card{NoteID: n.ID, DeckID: n.DeckID, Ord: o, Position: pos}
			pos++
			if err := insertCard(ctx, tx, uid, &c); err != nil {
				return err
			}
		}
	}
	for o := range have {
		if !want[o] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM cards WHERE note_id = ? AND user_id = ? AND ord = ?`, n.ID, uid, o); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// GetNote returns one of the person's notes with its cards.
func (s *Store) GetNote(ctx context.Context, uid, id int64) (*Note, error) {
	var deck string
	n, err := scanNote(s.DB.QueryRowContext(ctx, `SELECT `+noteCols+`, d.name FROM notes n JOIN decks d ON d.id = n.deck_id WHERE n.id = ? AND n.user_id = ?`, id, uid), &deck)
	if err != nil {
		return nil, err
	}
	n.DeckName = deck
	cards, err := s.cardsOf(ctx, uid, []int64{n.ID})
	if err != nil {
		return nil, err
	}
	n.Cards = cards[n.ID]
	return n, nil
}

// NoteByExternal finds a note by where it came from (an import's id).
func (s *Store) NoteByExternal(ctx context.Context, uid int64, source, ext string) (*Note, error) {
	return scanNote(s.DB.QueryRowContext(ctx, `SELECT `+noteCols+` FROM notes n WHERE n.user_id = ? AND n.source = ? AND n.external_id = ?`, uid, source, ext))
}

// DeleteNote removes a note with its cards.
func (s *Store) DeleteNote(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM notes WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// NoteFilter narrows the card browser.
type NoteFilter struct {
	DeckID int64
	Query  string
	Tag    string
	State  string // new | learning | review | due | suspended | flagged | hard (lapses ≥ 4)
	Source string
	Sort   string // created (newest first) | updated | front | due
	Limit  int
	Offset int
	DayEnd string // for State "due"
	Today  string
}

// ListNotes returns a page of the person's notes (with cards) and the total.
func (s *Store) ListNotes(ctx context.Context, uid int64, f NoteFilter) ([]Note, int, error) {
	where := []string{"n.user_id = ?"}
	args := []any{uid}
	if f.DeckID > 0 {
		where = append(where, "n.deck_id = ?")
		args = append(args, f.DeckID)
	}
	for _, w := range strings.Fields(f.Query) {
		if strings.HasPrefix(w, "#") && len(w) > 1 {
			where = append(where, "(' ' || n.tags || ' ') LIKE ?")
			args = append(args, "% "+strings.ToLower(w[1:])+" %")
			continue
		}
		where = append(where, "(n.front LIKE ? OR n.back LIKE ? OR n.extra LIKE ? OR n.tags LIKE ?)")
		like := "%" + w + "%"
		args = append(args, like, like, like, like)
	}
	if f.Tag != "" {
		where = append(where, "(' ' || n.tags || ' ') LIKE ?")
		args = append(args, "% "+strings.ToLower(f.Tag)+" %")
	}
	if f.Source != "" {
		where = append(where, "n.source = ?")
		args = append(args, f.Source)
	}
	cardCond := ""
	switch f.State {
	case "new":
		cardCond = "c.state = 0 AND c.suspended = 0"
	case "learning":
		cardCond = "c.state IN (1, 3) AND c.suspended = 0"
	case "review":
		cardCond = "c.state = 2 AND c.suspended = 0"
	case "due":
		cardCond = "c.suspended = 0 AND c.state IN (1, 2, 3) AND c.due < ?"
	case "suspended":
		cardCond = "c.suspended = 1"
	case "flagged":
		cardCond = "c.flag > 0"
	case "hard":
		cardCond = "c.lapses >= 4"
	}
	if cardCond != "" {
		where = append(where, "EXISTS (SELECT 1 FROM cards c WHERE c.note_id = n.id AND "+cardCond+")")
		if f.State == "due" {
			args = append(args, f.DayEnd)
		}
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM notes n WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "n.created_at DESC, n.id DESC"
	switch f.Sort {
	case "updated":
		order = "n.updated_at DESC, n.id DESC"
	case "front":
		order = "n.front COLLATE NOCASE, n.id"
	case "oldest":
		order = "n.created_at, n.id"
	case "due":
		order = "(SELECT MIN(CASE WHEN c.state = 0 THEN '9' ELSE c.due END) FROM cards c WHERE c.note_id = n.id), n.id"
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+noteCols+`, d.name FROM notes n JOIN decks d ON d.id = n.deck_id WHERE `+cond+` ORDER BY `+order+fmt.Sprintf(` LIMIT %d OFFSET %d`, f.Limit, max(f.Offset, 0)), args...)
	if err != nil {
		return nil, 0, err
	}
	out := []Note{}
	var ids []int64
	for rows.Next() {
		var deck string
		n, err := scanNote(rows, &deck)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		n.DeckName = deck
		out = append(out, *n)
		ids = append(ids, n.ID)
	}
	rows.Close()
	cards, err := s.cardsOf(ctx, uid, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].Cards = cards[out[i].ID]
	}
	return out, total, nil
}

// AllNotes returns every note of the person (for the export), oldest first.
func (s *Store) AllNotes(ctx context.Context, uid int64) ([]Note, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+noteCols+` FROM notes n WHERE n.user_id = ? ORDER BY n.id`, uid)
	if err != nil {
		return nil, err
	}
	out := []Note{}
	var ids []int64
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *n)
		ids = append(ids, n.ID)
	}
	rows.Close()
	cards, err := s.cardsOf(ctx, uid, nil)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Cards = cards[out[i].ID]
	}
	return out, nil
}

// TagCount is a tag with how many notes carry it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// Tags lists the person's tags, most used first.
func (s *Store) Tags(ctx context.Context, uid int64) ([]TagCount, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT tags FROM notes WHERE user_id = ? AND tags <> ''`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	n := map[string]int{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		for _, x := range strings.Fields(t) {
			n[x]++
		}
	}
	out := []TagCount{}
	for t, c := range n {
		out = append(out, TagCount{t, c})
	}
	sortTags(out)
	return out, rows.Err()
}

func sortTags(t []TagCount) {
	for i := 1; i < len(t); i++ {
		for j := i; j > 0 && (t[j].Count > t[j-1].Count || (t[j].Count == t[j-1].Count && t[j].Tag < t[j-1].Tag)); j-- {
			t[j], t[j-1] = t[j-1], t[j]
		}
	}
}

// MoveNotes puts notes (and their cards) into another deck.
func (s *Store) MoveNotes(ctx context.Context, uid int64, ids []int64, deckID int64) (int, error) {
	in, args := idList(ids)
	now := Now()
	res, err := s.DB.ExecContext(ctx, `UPDATE notes SET deck_id = ?, updated_at = ? WHERE user_id = ? AND id IN `+in, append([]any{deckID, now, uid}, args...)...)
	if err != nil {
		return 0, err
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE cards SET deck_id = ?, updated_at = ? WHERE user_id = ? AND note_id IN `+in, append([]any{deckID, now, uid}, args...)...); err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// TagNotes adds or removes a tag on notes.
func (s *Store) TagNotes(ctx context.Context, uid int64, ids []int64, tag string, add bool) (int, error) {
	tag = JoinTags(tag)
	if tag == "" || strings.Contains(tag, " ") {
		return 0, fmt.Errorf("one tag at a time")
	}
	in, args := idList(ids)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, tags FROM notes WHERE user_id = ? AND id IN `+in, append([]any{uid}, args...)...)
	if err != nil {
		return 0, err
	}
	type nt struct {
		id   int64
		tags string
	}
	var list []nt
	for rows.Next() {
		var x nt
		if err := rows.Scan(&x.id, &x.tags); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, x)
	}
	rows.Close()
	changed := 0
	for _, x := range list {
		cur := strings.Fields(x.tags)
		var next []string
		has := false
		for _, t := range cur {
			if t == tag {
				has = true
				if !add {
					continue
				}
			}
			next = append(next, t)
		}
		if add && !has {
			next = append(next, tag)
		}
		if (add && has) || (!add && !has) {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE notes SET tags = ?, updated_at = ? WHERE id = ? AND user_id = ?`, strings.Join(next, " "), Now(), x.id, uid); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// DeleteNotes removes notes.
func (s *Store) DeleteNotes(ctx context.Context, uid int64, ids []int64) (int, error) {
	in, args := idList(ids)
	res, err := s.DB.ExecContext(ctx, `DELETE FROM notes WHERE user_id = ? AND id IN `+in, append([]any{uid}, args...)...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
