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

-- What a person searched for and which results they opened. Only kept
-- while their history is on; pruned after keep_history_days.
CREATE TABLE history (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL DEFAULT 'search',  -- search | open
  query      TEXT NOT NULL DEFAULT '',
  category   TEXT NOT NULL DEFAULT '',        -- general | images | videos | news | apps
  url        TEXT NOT NULL DEFAULT '',        -- kind=open: the result opened
  title      TEXT NOT NULL DEFAULT '',
  results    INTEGER NOT NULL DEFAULT 0,      -- kind=search: how many came back
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX history_user_time ON history(user_id, created_at DESC);
CREATE INDEX history_user_query ON history(user_id, query);

-- A person's rule for one site: raise it, lower it, hide it, or pin it on
-- top. Matches the domain and every subdomain.
CREATE TABLE site_rules (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  domain     TEXT NOT NULL,
  action     TEXT NOT NULL,                   -- pin | boost | lower | block
  note       TEXT NOT NULL DEFAULT '',
  hits       INTEGER NOT NULL DEFAULT 0,      -- results it changed
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, domain)
);

-- A person's own bang: "!x words" jumps straight to url with {q} filled in.
CREATE TABLE bangs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  trigger    TEXT NOT NULL,                   -- without the "!"
  name       TEXT NOT NULL DEFAULT '',
  url        TEXT NOT NULL,                   -- contains {q}
  uses       INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, trigger)
);

-- Results a person sent somewhere (Reader, Notes, Lookout…): the result
-- page shows "Saved to Reader" again instead of offering it twice.
CREATE TABLE saves (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  app        TEXT NOT NULL,                   -- reader | notes | tasks | lookout | wishlist
  title      TEXT NOT NULL DEFAULT '',
  item_url   TEXT NOT NULL DEFAULT '',        -- where it landed
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, url, app)
);
CREATE INDEX saves_user_time ON saves(user_id, created_at DESC);
