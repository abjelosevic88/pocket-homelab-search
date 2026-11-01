# Backup & restore

Everything lives in `DATA_DIR` (`/data` in the container):

- `pocket-search.db` (+ `-wal` / `-shm` while running) — history, site rules, bangs, saves, settings, users, cached site icons
- `backups/` — where the CLI writes snapshots

The bundled search engine keeps nothing worth backing up.

## Consistent copies

Never copy a live WAL database file. Use one of:

- **UI**: Settings → Backup & data → *Download database*
- **API**: `GET /api/v1/backup` with a bearer token
- **CLI**: `docker exec pocket-search pocket-search backup` → prints the path under `/data/backups/`
- **sqlite3**: `sqlite3 data/pocket-search.db "VACUUM INTO 'snap.db'"`

Exclude `pocket-search.db*` from raw file backups and include the snapshot instead.

## Restore

1. Stop the container.
2. Replace `data/pocket-search.db` with the snapshot (remove any stale `-wal` / `-shm`).
3. Start the container. Migrations run forward automatically.

## JSON export

Settings → Backup & data exports your own site rules, bangs, history and saves as one JSON file. Importing it merges (rules by site, bangs by shortcut, history only when asked); importing twice changes nothing. A plain text list of sites (`example.com`, or `example.com raise|lower|pin|hide`) becomes site rules.
