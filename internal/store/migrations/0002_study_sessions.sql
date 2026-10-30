-- A study session: one run on the study screen, from starting it to
-- finishing or leaving it (15 minutes without an answer end it at the last
-- answer). Learn records its own; with Pocket Time connected the run is
-- timed there instead and Time writes the finished time back (source
-- 'time', external_id 'time:<entry id>') into the same row (timer_entry_id).
CREATE TABLE study_sessions (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  deck_id        INTEGER REFERENCES decks(id) ON DELETE SET NULL, -- NULL = all decks
  deck_name      TEXT NOT NULL DEFAULT '',     -- as it was, for a deleted deck
  started_at     TEXT NOT NULL,                -- RFC3339 with offset
  ended_at       TEXT NOT NULL DEFAULT '',     -- '' = still open
  last_at        TEXT NOT NULL DEFAULT '',     -- the last answer (or the start)
  date           TEXT NOT NULL,                -- local calendar date of started_at
  duration_s     INTEGER NOT NULL DEFAULT 0,
  cards          INTEGER NOT NULL DEFAULT 0,
  again          INTEGER NOT NULL DEFAULT 0,
  source         TEXT NOT NULL DEFAULT 'app',  -- app (Learn's own) | time | api | <pocket app slug>
  external_id    TEXT NOT NULL DEFAULT '',     -- 'time:<entry id>' for what Pocket Time wrote
  timer_entry_id INTEGER NOT NULL DEFAULT 0,   -- the Pocket Time timer timing this run (0 = none)
  title          TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX study_sessions_user_date ON study_sessions(user_id, date);
CREATE INDEX study_sessions_open ON study_sessions(ended_at) WHERE ended_at = '';
CREATE UNIQUE INDEX study_sessions_external ON study_sessions(user_id, external_id) WHERE external_id <> '';
CREATE INDEX study_sessions_timer ON study_sessions(user_id, timer_entry_id) WHERE timer_entry_id > 0;
