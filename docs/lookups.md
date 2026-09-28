# Dynamic lookups

A **lookup group** is a named table of text keys and text values: facility codes to names,
local codes to standard ones, and similar. Groups are stored with the flows (durable with
`store.dialect: postgres` or `sqlite`) and managed over the API.

!!! note "Stored, not used by flows yet"
    Flows cannot read lookups yet. Today these endpoints store and serve them.

## Permissions

Reading (list, match, get, batch, exists) needs `lookups:view`; changing (import, put, delete)
needs `lookups:edit`.

## Load a group

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST \
  http://127.0.0.1:8080/api/v1/lookups/facility/import \
  -d '{"LAB-01":"Central Lab","LAB-02":"North Lab","RAD 01":"Radiology"}'
```

```json
{"imported":3}
```

The body is a JSON object of key to text value. Entries are created or replaced, all or nothing;
other entries stay. Add `?replace=true` to make the body the whole group.

- Group names are 1–128 characters from `A-Z a-z 0-9 . _ -`.
- Keys are 1–512 characters of any text except control characters, `/` included. URL-encode
  them in paths (`RAD%2001`, `ICD%2F10`).
- Values are text of at most 64 KiB. A group exists while it has entries.

## Requests

| Request | What it does |
|---|---|
| `GET /api/v1/lookups` | Every group with its number of entries: `[{"name":"facility","entries":3}]`. |
| `GET /api/v1/lookups/{group}?prefix=LAB-&limit=100` | Entries whose key starts with `prefix` (all without it), the first `limit` (1–10000, default 1000) in key order, as `{key: value}`. |
| `GET /api/v1/lookups/{group}/{key}` | One entry: `{"key":…,"value":…}`, or `404`. |
| `GET /api/v1/lookups/{group}/{key}/exists` | `{"exists": true}` or `{"exists": false}` (never `404`). |
| `POST /api/v1/lookups/{group}/batch` | Body `{"keys": [...]}` (1–1000 keys): `{"found": {key: value}, "missing": [keys]}`. |
| `PUT /api/v1/lookups/{group}/{key}` | Body `{"value": "text"}`: creates or replaces one entry. |
| `DELETE /api/v1/lookups/{group}/{key}` | Deletes one entry (`204`, or `404`). |
| `DELETE /api/v1/lookups/{group}` | Deletes the whole group (`204`, or `404` when it has no entries). |
| `POST /api/v1/lookups/{group}/import[?replace=true]` | Stores many entries, as above. |

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST \
  http://127.0.0.1:8080/api/v1/lookups/facility/batch -d '{"keys":["LAB-01","XX"]}'
```

```json
{"found":{"LAB-01":"Central Lab"},"missing":["XX"]}
```
