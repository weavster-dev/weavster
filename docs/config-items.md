# Config map, scripts, and settings

The server keeps three sets of named values for you: the **config map** (text values, for example
hostnames per environment), **global scripts** (script sources), and **settings** (any JSON
value). They are stored with the flows (durable with `store.dialect: postgres`) and managed over the
API and the command-line client.

!!! note "Stored, not used yet"
    Flows do not read the config map, and global scripts are not run. Today these endpoints store
    and return the values, so you can keep them next to your flows and move them between servers.

## API

Each set has the same five operations. Names are 1–128 characters from `A-Z a-z 0-9 . _ -`.

| Request | What it does |
|---|---|
| `GET /api/v1/configmap` | Every entry, as a JSON object of `name: value`. |
| `PUT /api/v1/configmap` | Replaces the whole set with the body (a JSON object of `name: value`). |
| `GET /api/v1/configmap/{name}` | One entry: `{"name":…,"value":…}` (`404` if unknown). |
| `PUT /api/v1/configmap/{name}` | Creates or replaces one entry; the body is `{"value": …}`. |
| `DELETE /api/v1/configmap/{name}` | Deletes one entry (`204`; `404` if unknown). |

The same requests work on `/api/v1/scripts` and `/api/v1/settings`.

| Set | Values | Permission |
|---|---|---|
| `configmap` | strings | `configmap:edit` |
| `scripts` | strings (the script source) | `scripts:edit` |
| `settings` | any JSON value | `settings:edit` |

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X PUT http://127.0.0.1:8080/api/v1/configmap \
  -d '{"ehr.host":"ehr.example.com","region":"eu"}'
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X PUT http://127.0.0.1:8080/api/v1/settings/retention \
  -d '{"value":{"days":30}}'
```

A value of the wrong type (a number in the config map, for example), a `null` value, an unknown
field in the body, or a bad name returns `400` and changes nothing. Bodies may be up to 10 MiB.

## Command-line client

| Command | What it does |
|---|---|
| `exportmap "path"` | Writes the config map to a JSON file. |
| `importmap "path"` | Replaces the config map with the file's entries. |
| `exportscripts "path"` | Writes the global scripts to a JSON file. |
| `importscripts "path"` | Replaces the global scripts with the file's entries. |

The files are the same JSON object as `GET` returns, so you can move a config map between servers
with `exportmap` on one and `importmap` on the other.
