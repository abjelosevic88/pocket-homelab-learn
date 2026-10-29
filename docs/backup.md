# Backup

Two things: the SQLite file `DATA_DIR/pocket-learn.db` (WAL mode) and the pictures and sounds in `DATA_DIR/files/` (one folder per person, files named by their content hash).

Take a consistent copy of the database with `pocket-learn backup` (writes `DATA_DIR/backups/pocket-learn-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin). Back up that copy and `files/`, not the live database file. `DATA_DIR/core.token*` is the Pocket Core registration; `DATA_DIR/tmp/` holds interrupted Anki uploads and can be skipped.

Restore: stop the app, replace `pocket-learn.db` (remove `-wal`/`-shm` files) and `files/`, start.

Every person can also export their own data as JSON (`/api/v1/export.json` — decks, cards with their memory state and every answer; re-importable, importing twice adds nothing; `?deck=ID` for one deck) or the cards as CSV (`/api/v1/export.csv`).
