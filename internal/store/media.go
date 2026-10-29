package store

import "context"

const mediaCols = `id, user_id, name, hash, mime, size, created_at`

func scanMedia(row interface{ Scan(...any) error }) (*Media, error) {
	var m Media
	if err := row.Scan(&m.ID, &m.UserID, &m.Name, &m.Hash, &m.Mime, &m.Size, &m.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	return &m, nil
}

// GetMedia finds one of the person's files by name.
func (s *Store) GetMedia(ctx context.Context, uid int64, name string) (*Media, error) {
	return scanMedia(s.DB.QueryRowContext(ctx, `SELECT `+mediaCols+` FROM media WHERE user_id = ? AND name = ?`, uid, name))
}

// MediaByHash finds a file with the same content.
func (s *Store) MediaByHash(ctx context.Context, uid int64, hash string) (*Media, error) {
	return scanMedia(s.DB.QueryRowContext(ctx, `SELECT `+mediaCols+` FROM media WHERE user_id = ? AND hash = ? ORDER BY id LIMIT 1`, uid, hash))
}

// AddMedia records a file.
func (s *Store) AddMedia(ctx context.Context, uid int64, m *Media) error {
	m.CreatedAt = Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO media (user_id, name, hash, mime, size, created_at) VALUES (?, ?, ?, ?, ?, ?)`, uid, m.Name, m.Hash, m.Mime, m.Size, m.CreatedAt)
	if err != nil {
		return conflict(err)
	}
	m.ID, _ = res.LastInsertId()
	m.UserID = uid
	return nil
}

// ListMedia returns all of the person's files.
func (s *Store) ListMedia(ctx context.Context, uid int64) ([]Media, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+mediaCols+` FROM media WHERE user_id = ? ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// HashInUse reports whether any of anyone's media rows still points at a stored file.
func (s *Store) HashInUse(ctx context.Context, uid int64, hash string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM media WHERE user_id = ? AND hash = ?`, uid, hash).Scan(&n)
	return n > 0
}

// DeleteMedia removes a file's record.
func (s *Store) DeleteMedia(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM media WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
