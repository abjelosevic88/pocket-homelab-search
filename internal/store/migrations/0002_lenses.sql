-- A lens: a saved way of searching, one click under the search box
-- ("Recipes", "Code", "Local"). Only these sites, these sites raised, these
-- sites hidden, words added to every search, a language. Strictly per person.
CREATE TABLE lenses (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  icon       TEXT NOT NULL DEFAULT '',       -- an emoji
  sites      TEXT NOT NULL DEFAULT '',       -- only results from these (one domain per line; "ba" = every .ba site)
  boost      TEXT NOT NULL DEFAULT '',       -- these first
  hide       TEXT NOT NULL DEFAULT '',       -- never these
  words      TEXT NOT NULL DEFAULT '',       -- added to every search ("recipe")
  language   TEXT NOT NULL DEFAULT '',       -- '' = the person's own
  position   INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX lenses_user ON lenses(user_id, position, id);
