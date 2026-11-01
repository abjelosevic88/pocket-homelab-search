# API

JSON over HTTP under `/api/v1`. Authenticate with the session cookie (the UI), or `Authorization: Bearer psr_…` (Settings → API & shortcuts). Everything is the caller's own: other people's history, rules and bangs are `404`.

## Searching

| | |
|---|---|
| `GET /search?q=&cat=&time=&page=&lang=` | Web search. `cat` = `general` (default: the person's), `images`, `videos`, `news`, `music`, `it`, `science`, `files`, `map`, `social media`; `time` = `day`/`week`/`month`/`year`; `page` 1–20. Answer: `results` (`url`, `title`, `content`, `domain`, `engines`, `published`, `thumbnail`, `image`, `rule` = `pin`/`boost`/`lower` when a site rule moved it, `saved` = where you sent it), `answers`, `infoboxes`, `suggestions`, `corrections`, `unresponsive`, `hidden` + `hidden_sites` (your rules), `more`. A search with a bang answers `{"redirect": "…"}` instead. `nohistory=1` keeps it out of history; `terms=` works like `q=` and never adds history (Pocket Core's AI tool uses it). |
| `GET /search?…&lens=<id>` | Search through a lens: only its sites (asked as `site:` filters and filtered again), its sites first or hidden, its words added, its language. |
| `instant` in a search's answer | On the All tab's first page: a sum (`15% of 240`, `(3+4)*2^3`), a conversion (`5 km in miles`, `100 f to c`), a currency (`100 eur to bam`, rates via open.er-api.com, BAM pegged), the time in a city (`time in tokyo`), the weather (`weather sarajevo`, Open-Meteo), or from your Pocket apps: `my tasks today` (Tasks), `next trip` (Travel), `my calendar today` / `tomorrow` (Calendar). `{kind, title, value, detail, rows, source, url}`. |
| `GET /search/apps?q=&app=` | Your Pocket apps, searched at once: `sections` (`app`, `name`, `items` with `title`, `snippet`, `date`, `url` = the item in the app, `kind`; `error` / `off` when an app is not connected). |
| `GET /suggest?q=` | `history`, `suggestions`, and `bangs` while typing `!x`. `format=opensearch` answers browsers. |
| `POST /answer` `{"q", "cat", "time"}` | AI answer from the results: `answer` (with `[n]` citations), `sources`, `provider`. |
| `GET /read?url=` | The readable article of a page, fetched by the server; pictures point at `/img`. `422` = no article. |
| `GET /img?u=` · `GET /favicon?d=` | A picture / a site's icon through the server (pictures only). |
| `GET /engines` | Engines per category and whether you use each. |
| `GET /search?q=` (no `/api/v1`) · `GET /opensearch.xml` | The browser search-engine address (bangs jump server-side) and its description. |

## Yours

`GET /history?q=&kind=search|open&limit=&offset=` · `DELETE /history/{id}` · `POST /history/clear {"range": "hour|day|week|all"}` · `POST /history/open {url, title, query}`

`GET /rules` · `POST /rules {domain, action: pin|boost|lower|block, note}` (adds or changes the site's rule) · `PUT /rules/{id}` · `DELETE /rules/{id}`

`GET /lenses` (five to start with: Recipes, Code, Forums, Local, Science) · `POST /lenses {name, icon, sites, boost, hide, words, language}` · `PUT /lenses/{id}` · `DELETE /lenses/{id}`

`GET /bangs` (`mine`, `builtin`) · `POST /bangs {trigger, name, url with {q}}` · `PUT /bangs/{id}` · `DELETE /bangs/{id}`

`GET /apps` (connected Pocket apps and how) · `POST /apps/{slug}/disconnect` · `POST /send/{app} {url, title, snippet, query, note}` (Reader, Notes, Tasks, Lookout, Wishlist) · `GET /saves`

`GET /dashboard` (searches today/this week, rules, bangs, saves, recent searches — for Homepage widgets) · `GET /export.json` · `POST /import` (an export, or a text list of sites) · `GET /backup` (admin) · `GET/PUT /settings`

## Pocket Core

Search offers one scope to other Pocket apps (switched on per app and person in Pocket Core → Links):

- `search:read` — `GET /search`, `/suggest`, `/read`: web searches and page text for the person, with their engines and site rules; nothing is added to their history.

It uses (each off until switched on): `notes:read/write`, `reading:read/write`, `tasks:read/write`, `watches:read/write`, `wishes:read/write`, `documents:read`, `calendar:read`, `contacts:read`, `projects:read`, `boards:read`, `books:read`, `library:read`, `items:read`, `mail:read`, `history:read` (Podcasts), `guitar:read`, `drawings:read`, `trips:read`, `playlists:write` (Music search), and `inference:write` (Pocket AI). Pocket Core's AI tools `pocket_search_web` and `pocket_search_read_page` reach it too.

Event on the Core bus: `search.saved` (`app`, `url`, `title`, `query`, `item_url`, `user_email`) when a result is sent to an app.
