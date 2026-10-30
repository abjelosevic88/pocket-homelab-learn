# API

JSON over HTTP under `/api/v1`. Authenticate with a session cookie (the web UI), a personal token (`Authorization: Bearer pln_…`, Settings → API & shortcuts) or a Pocket Core token. Everything is per person: another person's ids answer 404.

## Today and statistics

| Call | What |
|---|---|
| `GET /dashboard` | Cards due now (`due`, `due_new`, `due_learn`, `due_review`), studied today, minutes, streak — for Homepage |
| `GET /home` | The Today page: dashboard, decks with counts, the last 12 weeks, the next 7 days |
| `GET /stats?range=w\|m\|3m\|y\|all&deck=` | Answers per day, retention (young/mature), forecast, intervals, hours, hardest cards, streak |
| `GET /day/YYYY-MM-DD` | One study day: answers, new cards, minutes, decks, retention — what Pocket Day reads |

## Decks

| Call | What |
|---|---|
| `GET /decks[?archived=1]` | Decks with today's counts |
| `POST /decks` | `{"name","description","color","new_per_day","reviews_per_day","retention","new_order":"added\|random"}` |
| `GET/PUT/DELETE /decks/{id}` | One deck (PUT takes any of the fields; deleting removes its cards and history) |
| `POST /decks/reorder` | `{"ids":[…]}` |
| `POST /decks/{id}/extend` | `{"new":10}` or `{"reviews":50}` — study more today |

## Notes and cards

A note is what you write; it makes one card (`basic`, `type`), two (`reverse`) or one per cloze gap (`cloze`, `{{c1::answer::hint}}` in the front). Text is plain "markdown-lite": `**bold**`, `*italic*`, `` `code` ``, lists, `![](media/name.png)`, `[sound:name.mp3]`.

| Call | What |
|---|---|
| `GET /notes?deck=&q=&tag=&state=new\|learning\|review\|due\|suspended\|flagged\|hard&sort=&limit=&offset=` | Browse, with each note's cards |
| `POST /notes` | `{"deck_id" or "deck":"name","kind","front","back","extra","tags","source_ref","source_url","external_id"}` — `deck` by name is created when missing; an `external_id` seen before returns the existing note with `"duplicate":true` |
| `GET/PUT/DELETE /notes/{id}` | One note; PUT takes any field, cards follow the kind and the cloze gaps |
| `POST /notes/bulk` | `{"ids":[…],"action":"move\|tag\|untag\|suspend\|unsuspend\|forget\|delete","deck_id","tag"}` |
| `GET /tags` | Tags with counts |
| `POST /cards/{id}/action` | `{"action":"bury\|unbury\|suspend\|unsuspend\|forget\|flag","flag":0-4}` |
| `GET /cards/{id}/history` | A card's answers and its chance of being remembered now |

## Studying

| Call | What |
|---|---|
| `GET /study/next?deck=` | The next card (with the four answers' intervals), what is left today, or `come_back` when only a learning card later today is left |
| `POST /study/start` | `{"deck","switch"}` → the study session (`session`), or `conflict`: the timer running in Pocket Time (`switch:true` stops it and starts this) |
| `POST /study/answer` | `{"card_id","rating":1-4,"duration_ms","deck","session_id"}` → the review id, the card, the next card, the session |
| `POST /study/undo` | `{"review_id","deck","session_id"}` — take the last answer of a card back |
| `POST /study/stop` | `{"session_id"}` — end the session (the Stop button, leaving the screen) |
| `POST /study/finish` | `{"deck","cards","again","duration_ms","session_id"}` — ends the session, publishes `learn.session_finished` |
| `GET /time/timer?session=` | The timer running in Pocket Time (`running`, `mine`), and that session's state |
| `POST /time/timer/stop` | `{"id"}` — stop that timer in Pocket Time (only if it is still the one running) |

A **study session** runs from the first card on the study screen to finishing, Stop or leaving it; 15 minutes without an answer end it at the last answer. Without Pocket Time Learn records it itself (`source` `app`). With Pocket Time connected (Settings → Connected apps) the study timer is the person's one timer there: the session starts it (`app_ref` `deck:<id>`), ending it stops it, and Pocket Time writes the time back through `POST /worklog` into the same session — never twice.

## Worklog (for Pocket Time)

The contract in pocket-homelab-time `docs/worklog.md`. Refs are `deck:<id>` (or `deck:all`).

| Call | What |
|---|---|
| `GET /worklog?from=&to=` | Your study sessions in a period: `{"entries":[{"id","title","started_at","ended_at","date","duration_s","ref","label","url","source","external_id"}]}` |
| `GET /worklog/items?q=` | The decks to log time for, studied lately first |
| `POST /worklog` | `{"ref","title","started_at","ended_at","date","duration_s","external_id"}` → `{"id","url"}`; the same `external_id` again updates it |
| `DELETE /worklog/{id}` | Remove a session the caller logged (never Learn's own) |

## Import, export, media

| Call | What |
|---|---|
| `POST /import/anki?progress=0\|1` | multipart `file`: an `.apkg`/`.colpkg` (up to `MAX_UPLOAD_BYTES`), with pictures, sounds and (by default) each card's progress |
| `POST /import/csv` | `{"text","delimiter":"auto\|,\|;\|tab","header","kind","front":0,"back":1,"extra":-1,"tags_col":-1,"tags","deck_id" or "deck","dry_run"}` |
| `GET /export.json[?deck=]`, `POST /import` | Everything of yours, re-importable |
| `GET /export.csv` | The cards as CSV |
| `POST /media` (multipart `file`), `GET /media/{name}` | Pictures and sounds on cards |

## Other Pocket apps and AI

| Call | What |
|---|---|
| `GET /sources/reader\|library\|notes?q=` | Your highlights in Pocket Reader / Pocket Library, your notes in Pocket Notes |
| `GET /sources/notes/{id}` | One note's text |
| `POST /sources/highlights` | `{"deck_id","items":[…]}` — highlights as cards, as they are |
| `POST /ai/cards` | `{"text","title","count","kind":"auto\|basic\|cloze","language","focus"}` → suggested cards (nothing saved) |
| `POST /ai/explain` | `{"card_id"}` → an explanation and a memory aid |
| `POST /ai/ask` | `{"question"}` about your studying |

## Pocket Core links

Offers `learn:read` (dashboard, stats, decks, day — never card content), `learn:write` (add notes; change or delete only notes that app added), `worklog:read` (study sessions, the decks to log for) and `worklog:write` (log study time; change or remove only what that app logged). Uses `reading:read` from Pocket Reader and Pocket Library, `notes:read` from Pocket Notes and `time:read`/`time:write` from Pocket Time (the study timer). Publishes `learn.session_finished`, `learn.all_done`, `learn.cards_added`, `learn.streak_reached`.
