package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// StudySession is one run on the study screen (or time logged for a deck).
type StudySession struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"-"`
	DeckID       *int64 `json:"deck_id"`
	DeckName     string `json:"deck_name"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at"`
	LastAt       string `json:"last_at"`
	Date         string `json:"date"`
	DurationS    int    `json:"duration_s"`
	Cards        int    `json:"cards"`
	Again        int    `json:"again"`
	Source       string `json:"source"`
	ExternalID   string `json:"external_id"`
	TimerEntryID int64  `json:"timer_entry_id"`
	Title        string `json:"title"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// Open reports whether the session is still running here.
func (x *StudySession) Open() bool { return x.EndedAt == "" }

// KeptByTime reports whether Pocket Time times this run and has not written
// its time back yet: Learn holds no time of its own for it.
func (x *StudySession) KeptByTime() bool { return x.TimerEntryID > 0 && x.Source == "app" }

// Stamp is a moment as stored for sessions: RFC3339 with the local offset.
func Stamp(t time.Time) string { return t.In(time.Local).Format(time.RFC3339) }

// ParseStamp reads any RFC3339 moment.
func ParseStamp(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// LocalDate is the local calendar date of a moment.
func LocalDate(t time.Time) string { return t.In(time.Local).Format("2006-01-02") }

const sessionCols = `id, user_id, deck_id, deck_name, started_at, ended_at, last_at, date, duration_s, cards, again, source, external_id, timer_entry_id, title, created_at, updated_at`

func scanSession(row interface{ Scan(...any) error }) (*StudySession, error) {
	var x StudySession
	var deck sql.NullInt64
	err := row.Scan(&x.ID, &x.UserID, &deck, &x.DeckName, &x.StartedAt, &x.EndedAt, &x.LastAt, &x.Date, &x.DurationS, &x.Cards, &x.Again,
		&x.Source, &x.ExternalID, &x.TimerEntryID, &x.Title, &x.CreatedAt, &x.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	if deck.Valid {
		x.DeckID = &deck.Int64
	}
	return &x, nil
}

func deckArg(id *int64) any {
	if id == nil || *id <= 0 {
		return nil
	}
	return *id
}

// CreateStudySession adds a session for a person.
func (s *Store) CreateStudySession(ctx context.Context, uid int64, x *StudySession) error {
	if x.Source == "" {
		x.Source = "app"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO study_sessions (user_id, deck_id, deck_name, started_at, ended_at, last_at, date, duration_s, cards, again, source, external_id, timer_entry_id, title)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, deckArg(x.DeckID), x.DeckName, x.StartedAt, x.EndedAt, x.LastAt, x.Date, x.DurationS, x.Cards, x.Again, x.Source, x.ExternalID, x.TimerEntryID, x.Title)
	if err != nil {
		return conflict(err)
	}
	x.ID, _ = res.LastInsertId()
	x.UserID = uid
	return nil
}

// SaveStudySession writes every field of one of the person's sessions.
func (s *Store) SaveStudySession(ctx context.Context, uid int64, x *StudySession) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE study_sessions SET deck_id = ?, deck_name = ?, started_at = ?, ended_at = ?, last_at = ?, date = ?, duration_s = ?, cards = ?, again = ?,
		source = ?, external_id = ?, timer_entry_id = ?, title = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		deckArg(x.DeckID), x.DeckName, x.StartedAt, x.EndedAt, x.LastAt, x.Date, x.DurationS, x.Cards, x.Again, x.Source, x.ExternalID, x.TimerEntryID, x.Title, Now(), x.ID, uid)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetStudySession is one of the person's sessions.
func (s *Store) GetStudySession(ctx context.Context, uid, id int64) (*StudySession, error) {
	return scanSession(s.DB.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM study_sessions WHERE id = ? AND user_id = ?`, id, uid))
}

// SessionByExternal finds a person's session by its external id.
func (s *Store) SessionByExternal(ctx context.Context, uid int64, ext string) (*StudySession, error) {
	return scanSession(s.DB.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM study_sessions WHERE user_id = ? AND external_id = ?`, uid, ext))
}

// SessionByTimer finds the run a Pocket Time timer timed (the newest).
func (s *Store) SessionByTimer(ctx context.Context, uid, entry int64) (*StudySession, error) {
	return scanSession(s.DB.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM study_sessions WHERE user_id = ? AND timer_entry_id = ? ORDER BY id DESC LIMIT 1`, uid, entry))
}

// DeleteStudySession removes one of the person's sessions.
func (s *Store) DeleteStudySession(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM study_sessions WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// OpenStudySessions are the sessions still running (all people: background work),
// or one person's (uid > 0).
func (s *Store) OpenStudySessions(ctx context.Context, uid int64) ([]StudySession, error) {
	q, args := `SELECT `+sessionCols+` FROM study_sessions WHERE ended_at = ''`, []any{}
	if uid > 0 {
		q += ` AND user_id = ?`
		args = append(args, uid)
	}
	return s.listSessions(ctx, q+` ORDER BY id`, args...)
}

// SessionFilter narrows a person's finished sessions.
type SessionFilter struct {
	From, To string // local dates, inclusive ("" = open)
	Deck     int64  // 0 = all
	Limit    int
	// Own leaves out the runs Pocket Time is still keeping (no time here yet).
	Own bool
}

// ListStudySessions returns a person's finished sessions, newest first.
func (s *Store) ListStudySessions(ctx context.Context, uid int64, f SessionFilter) ([]StudySession, error) {
	var b strings.Builder
	b.WriteString(`SELECT ` + sessionCols + ` FROM study_sessions WHERE user_id = ? AND ended_at <> ''`)
	args := []any{uid}
	if f.From != "" {
		b.WriteString(` AND date >= ?`)
		args = append(args, f.From)
	}
	if f.To != "" {
		b.WriteString(` AND date <= ?`)
		args = append(args, f.To)
	}
	if f.Deck > 0 {
		b.WriteString(` AND deck_id = ?`)
		args = append(args, f.Deck)
	}
	if f.Own {
		b.WriteString(` AND NOT (timer_entry_id > 0 AND source = 'app')`)
	}
	b.WriteString(` ORDER BY started_at DESC, id DESC`)
	if f.Limit > 0 {
		b.WriteString(` LIMIT ?`)
		args = append(args, f.Limit)
	}
	return s.listSessions(ctx, b.String(), args...)
}

func (s *Store) listSessions(ctx context.Context, q string, args ...any) ([]StudySession, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StudySession{}
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// DeckActivity is when each of a person's decks was last studied (sessions or answers).
func (s *Store) DeckActivity(ctx context.Context, uid int64) (map[int64]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT deck_id, MAX(day) FROM reviews WHERE user_id = ? GROUP BY deck_id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var d string
		if err := rows.Scan(&id, &d); err != nil {
			return nil, err
		}
		out[id] = d
	}
	return out, rows.Err()
}
