# Configuration

Everything is read from environment variables; every value has a default, so a bare `docker run -v ./data:/data` works (then only your Pocket apps can be searched — add the engine, see below). Values marked *owner* seed the first admin's own settings once on first start; after that Settings wins.

| Variable | Default | What it does |
|---|---|---|
| `PORT` / `HOST` | `8080` / `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL: the browser search-engine address, the OIDC callback, links |
| `DATA_DIR` | `./data` (`/data` in the image) | SQLite database and backups |
| `DB_PATH` | `$DATA_DIR/pocket-search.db` | Override the location |
| `TZ` | – | Container timezone |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | `debug`, `warn`, `error`; `json` |
| `TRUST_PROXY` | `true` | Honour `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | Prometheus `/metrics` (row counts and a search counter, never queries) |
| `MAX_BODY_BYTES` | `16777216` | Largest import |
| `AUTH_MODE` | `local` | `local` (built-in accounts), `proxy` (trust `AUTH_PROXY_HEADER`), `none` (single user, LAN only) |
| `AUTH_PROXY_HEADER` / `AUTH_PROXY_EMAIL_HEADER` | `Remote-User` / `Remote-Email` | Headers for proxy mode |
| `SESSION_TTL` | `720h` | Cookie lifetime |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS (on plain http the browser drops the cookie and login bounces) |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | – | Sign in with an OpenID Connect provider. Needs `BASE_URL`; the callback is `BASE_URL/api/v1/auth/oidc/callback`. With Pocket Core none of these are needed. |
| `OIDC_NAME` / `OIDC_AUTO_CREATE` | `single sign-on` / `true` | Button label; give an unknown verified email a member account |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | – | Create the first admin on an empty database (otherwise the UI asks) |
| `SEARXNG_URL` | – | The search engine: a [SearXNG](https://github.com/searxng/searxng) with the JSON format on. The bundled one in `docker-compose.yml` is `http://engine:8080`. Your own works too — its `settings.yml` needs `search.formats: [html, json]` and, if only this app uses it, `server.limiter: false` |
| `ENGINE_TIMEOUT` | `8s` | How long a search may wait for the engines |
| `FETCH_PRIVATE` | `false` | Let the reading view and the picture proxy open addresses on private networks / loopback / tailnet. Off, nobody can make the server probe your LAN |
| `OPENAI_API_KEY` / `OPENAI_MODEL` / `OPENAI_BASE_URL` | – / `gpt-4o-mini` / OpenAI | *Owner.* An OpenAI-compatible AI for answers. With Pocket Core, Pocket AI is used instead when the `inference:write` link is on |
| `USER_AGENT` | a desktop Chrome string + `PocketSearch/1.0` | Sent when the server fetches pages and pictures |
| `CORE_URL` / `CORE_REGISTRATION_KEY` | – | [Pocket Core](https://github.com/abjelosevic88/pocket-homelab-core): register on start, sign in with Pocket Core, Pocket AI, your Pocket apps in the results, offer `search:read`, publish `search.saved` |
| `CORE_APP_URL` | `BASE_URL` | How Core reaches this app |
| `DEMO_DATA` | `false` | Seed months of history, site rules and bangs into an empty owner account. Without `SEARXNG_URL` searches answer with made-up results (for trying the app and screenshots) |

## The bundled engine

`docker-compose.yml` starts the official `searxng/searxng` image next to the app with its settings inline (no file to edit): JSON on, its rate limiter off (only the app asks it), no port of its own. Engine updates arrive with the image. People switch engines on and off for themselves in Settings → Engines; the app then names the engines in each search.

## What happens when you search

1. **Bangs first** — `!w eiffel tower` (or `eiffel tower !w`) jumps straight to the site; your own bangs win over the built-in ones. From the browser's address bar the jump happens on the server, before the page loads.
2. **The engines** — SearXNG asks the engines of the category at once (with your safe search, language, time range and engines) and merges the answers.
3. **Your rules** — sites you pinned come first, then raised ones, then the rest, then lowered ones; hidden sites are left out (the page says how many).
4. **Your apps** — at the same time each connected Pocket app is searched for the same words (a few seconds each, at most); matches appear beside the results.
5. **AI answer** — with a button or automatically: the AI reads the first results (and the text of the top three pages) and answers in a few lines, every claim numbered to its source.

Pictures, thumbnails and site icons are fetched by the server, and pages open with `Referrer-Policy: no-referrer`, so sites do not learn what you searched for.

## CLI

```
pocket-search                     start the server (default)
pocket-search backup [-o FILE]    write a consistent SQLite backup
pocket-search reset-password [-email USER] [-password NEW]
pocket-search version
```

Inside the container: `docker exec pocket-search pocket-search backup`.
