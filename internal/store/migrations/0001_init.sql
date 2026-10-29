CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);


-- A deck: a set of cards studied together, with its own daily limits.
CREATE TABLE decks (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name            TEXT NOT NULL,
  description     TEXT NOT NULL DEFAULT '',
  color           TEXT NOT NULL DEFAULT '',
  new_per_day     INTEGER NOT NULL DEFAULT 20,
  reviews_per_day INTEGER NOT NULL DEFAULT 200,
  retention       REAL NOT NULL DEFAULT 0,        -- 0 = the person's default
  new_order       TEXT NOT NULL DEFAULT 'added',  -- added | random
  archived        INTEGER NOT NULL DEFAULT 0,     -- hidden from Today, not studied
  sort            INTEGER NOT NULL DEFAULT 0,
  source          TEXT NOT NULL DEFAULT 'app',    -- app | anki | csv | import | <pocket app slug>
  external_id     TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX decks_user ON decks(user_id, archived, sort);
CREATE UNIQUE INDEX decks_user_name ON decks(user_id, name);

-- A note is what you write: a question and its answer. It makes one card
-- (basic, type), two (reverse: front→back and back→front) or one per cloze.
CREATE TABLE notes (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  deck_id     INTEGER NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL DEFAULT 'basic',   -- basic | reverse | cloze | type
  front       TEXT NOT NULL DEFAULT '',
  back        TEXT NOT NULL DEFAULT '',
  extra       TEXT NOT NULL DEFAULT '',        -- shown below the answer
  tags        TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT 'app',     -- app | ai | anki | csv | import | <pocket app slug>
  source_ref  TEXT NOT NULL DEFAULT '',        -- where it came from: an article, a book, a note
  source_url  TEXT NOT NULL DEFAULT '',
  external_id TEXT NOT NULL DEFAULT '',        -- an Anki guid, a highlight id
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX notes_user_deck ON notes(user_id, deck_id);
CREATE INDEX notes_user_created ON notes(user_id, created_at);
CREATE UNIQUE INDEX notes_external ON notes(user_id, source, external_id) WHERE external_id <> '';

-- A card is one thing to remember, with its FSRS memory state.
CREATE TABLE cards (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  note_id      INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  deck_id      INTEGER NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
  ord          INTEGER NOT NULL DEFAULT 0,     -- 0 front→back, 1 back→front, n = cloze n
  state        INTEGER NOT NULL DEFAULT 0,     -- 0 new, 1 learning, 2 review, 3 relearning
  step         INTEGER NOT NULL DEFAULT 0,
  stability    REAL NOT NULL DEFAULT 0,
  difficulty   REAL NOT NULL DEFAULT 0,
  due          TEXT NOT NULL DEFAULT '',       -- RFC3339 UTC; '' for new cards
  last_review  TEXT NOT NULL DEFAULT '',
  interval     INTEGER NOT NULL DEFAULT 0,     -- days, for review cards
  reps         INTEGER NOT NULL DEFAULT 0,
  lapses       INTEGER NOT NULL DEFAULT 0,
  position     INTEGER NOT NULL DEFAULT 0,     -- order of new cards
  suspended    INTEGER NOT NULL DEFAULT 0,
  buried_until TEXT NOT NULL DEFAULT '',       -- study day (YYYY-MM-DD) it comes back
  flag         INTEGER NOT NULL DEFAULT 0,     -- 0 none, 1 red, 2 orange, 3 green, 4 blue
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX cards_note_ord ON cards(note_id, ord);
CREATE INDEX cards_user_deck_state ON cards(user_id, deck_id, state, due);
CREATE INDEX cards_user_new ON cards(user_id, deck_id, state, position);

-- Every answer. prev holds the card as it was, so the last answer can be undone.
CREATE TABLE reviews (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  card_id        INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  deck_id        INTEGER NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
  rating         INTEGER NOT NULL,              -- 1 again, 2 hard, 3 good, 4 easy
  state          INTEGER NOT NULL,              -- the card's state before the answer
  elapsed_days   INTEGER NOT NULL DEFAULT 0,
  last_interval  INTEGER NOT NULL DEFAULT 0,    -- the card's interval before (21+ = a mature card)
  interval       INTEGER NOT NULL DEFAULT 0,    -- days given (0 = a learning step)
  stability      REAL NOT NULL DEFAULT 0,       -- after
  difficulty     REAL NOT NULL DEFAULT 0,
  retrievability REAL NOT NULL DEFAULT 0,       -- the chance of remembering it, before
  duration_ms    INTEGER NOT NULL DEFAULT 0,
  reviewed_at    TEXT NOT NULL,
  day            TEXT NOT NULL,                 -- the study day (day start hour applied)
  prev           TEXT NOT NULL DEFAULT ''       -- JSON of the card before
);
CREATE INDEX reviews_user_day ON reviews(user_id, day);
CREATE INDEX reviews_user_card ON reviews(user_id, card_id);
CREATE INDEX reviews_user_deck_day ON reviews(user_id, deck_id, day);

-- Pictures and sounds used on cards (an Anki import, a pasted picture).
CREATE TABLE media (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  hash       TEXT NOT NULL,
  mime       TEXT NOT NULL DEFAULT '',
  size       INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX media_user_name ON media(user_id, name);
CREATE INDEX media_user_hash ON media(user_id, hash);
