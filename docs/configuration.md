# Configuration

Environment variables; every one has a default. Personal settings (how sure to be, daily limits, reminders, connections, notifications) live in the UI per person — the `*_URL`/`*_TOKEN`/`OPENAI_*`/`NOTIFY_*` values below are copied once into the first admin's settings.

| Variable | Default | What |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL (links in pushes, the OIDC callback) |
| `DATA_DIR` | `./data` | Database, pictures and sounds (`files/`), Core token, backups |
| `DB_PATH` | `$DATA_DIR/pocket-learn.db` | SQLite file |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON request, e.g. an export being imported (64 MB) |
| `MAX_UPLOAD_BYTES` | `1073741824` | Largest Anki package upload (1 GB); raise your reverse proxy's limit too |
| `FILES_DIR` | `$DATA_DIR/files` | Pictures and sounds used on cards |
| `TZ` | – | Time zone for reminders and the study day |
| `AUTH_MODE` | `local` | `local` (accounts), `proxy` (trust `AUTH_PROXY_HEADER`/`AUTH_PROXY_EMAIL_HEADER`), `none` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | Seed the first admin while the database is empty |
| `SESSION_TTL`, `SECURE_COOKIES` | `720h`, `false` | Set `SECURE_COOKIES=true` behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `NOTIFY_URL`, `NOTIFY_FORMAT` | – | Apprise (`…/notify/key`), ntfy topic URL or JSON webhook; format `apprise`/`ntfy`/`json` |
| `READER_URL`, `READER_TOKEN` | – | Pocket Reader, direct connection: your article highlights become cards (or use Pocket Core) |
| `LIBRARY_URL`, `LIBRARY_TOKEN` | – | Pocket Library, direct connection: your book highlights become cards (or use Pocket Core) |
| `NOTES_URL`, `NOTES_TOKEN` | – | Pocket Notes, direct connection: make cards from a note (or use Pocket Core) |
| `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` | – | Cards made by AI, explanations and questions without Pocket AI |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | – | Pocket Core |
| `TRUST_PROXY`, `METRICS_ENABLED`, `LOG_LEVEL`, `LOG_FORMAT` | `true`, `true`, `info`, `text` | |
| `DEMO_DATA` | `false` | Seed five decks and four months of plausible studying into the owner account (screenshots, trying it out) |

Commands: `pocket-learn backup [-o FILE]`, `pocket-learn reset-password [-email] [-password]`, `pocket-learn version`.
