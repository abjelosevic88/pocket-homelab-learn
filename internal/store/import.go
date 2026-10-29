package store

import "context"

// ImportItem is a note with its cards as they should be (an Anki card keeps
// its progress, an exported card its memory state).
type ImportItem struct {
	Note  Note
	Cards []Card
	// Reviews to restore for these cards, by the card's ord.
	Reviews map[int][]Review
}

// ImportBatch adds many notes in one transaction. Notes whose source and
// external id already exist are skipped (counted in the second value); new
// cards are put at the end of the new-card order in the order given.
func (s *Store) ImportBatch(ctx context.Context, uid int64, items []ImportItem) (created, skipped int, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	pos := nextPosition(ctx, tx, uid)
	for i := range items {
		it := &items[i]
		n := &it.Note
		if n.ExternalID != "" {
			var id int64
			if e := tx.QueryRowContext(ctx, `SELECT id FROM notes WHERE user_id = ? AND source = ? AND external_id = ?`, uid, n.Source, n.ExternalID).Scan(&id); e == nil {
				skipped++
				continue
			}
		}
		now := Now()
		if n.CreatedAt == "" {
			n.CreatedAt = now
		}
		n.Tags = JoinTags(n.Tags)
		res, e := tx.ExecContext(ctx, `INSERT INTO notes (user_id, deck_id, kind, front, back, extra, tags, source, source_ref, source_url, external_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, n.DeckID, n.Kind, n.Front, n.Back, n.Extra, n.Tags, n.Source, n.SourceRef, n.SourceURL, n.ExternalID, n.CreatedAt, now)
		if e != nil {
			return 0, 0, conflict(e)
		}
		n.ID, _ = res.LastInsertId()
		for j := range it.Cards {
			c := &it.Cards[j]
			c.NoteID, c.DeckID = n.ID, n.DeckID
			if c.State == StateNew {
				c.Position = pos
				pos++
			}
			if e := insertCard(ctx, tx, uid, c); e != nil {
				return 0, 0, e
			}
			for _, r := range it.Reviews[c.Ord] {
				r.CardID, r.DeckID, r.Prev = c.ID, n.DeckID, ""
				if e := insertReview(ctx, tx, uid, &r); e != nil {
					return 0, 0, e
				}
			}
		}
		created++
	}
	return created, skipped, tx.Commit()
}

// RestoreState writes many cards' state and their reviews in one
// transaction (the demo data, a bulk restore).
func (s *Store) RestoreState(ctx context.Context, uid int64, cards []Card, reviews []Review) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i := range cards {
		if err := saveCard(ctx, tx, uid, &cards[i]); err != nil {
			return err
		}
	}
	for i := range reviews {
		if err := insertReview(ctx, tx, uid, &reviews[i]); err != nil {
			return err
		}
	}
	return tx.Commit()
}
