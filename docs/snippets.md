# Code snippets and libraries

**Code snippets** are named pieces of code you keep on the server to reuse. You can group them
into **snippet libraries**. Both are stored with the flows (durable with `store.dialect: postgres`)
and managed over the API and the command-line client. Every request needs the `snippets:edit`
permission.

!!! note "Stored, not used yet"
    Flows do not run snippets yet. Today these endpoints store and return them, so you can keep
    them next to your flows and move them between servers.

## Shapes

```json
{"name": "pid", "library": "hl7", "description": "Patient id", "code": "get('PID.3')"}
```

```json
{"name": "hl7", "description": "HL7 helpers"}
```

- `name` is required: 1–128 characters from `A-Z a-z 0-9 . _ -`.
- A snippet's `library` is optional. When set, that library must already exist.
- Unknown fields are rejected with `400`.

## API

| Request | What it does |
|---|---|
| `GET /api/v1/snippets` | Every snippet, sorted by name. Add `?summary=true` to leave out `code`. |
| `POST /api/v1/snippets` | Creates one snippet (`201`; `409` if the name is taken). |
| `PUT /api/v1/snippets` | Creates or replaces every snippet in the body (a JSON array). Snippets not in the body are kept. All or nothing: one bad entry changes nothing. |
| `GET /api/v1/snippets/{name}` | One snippet (`404` if unknown). |
| `PUT /api/v1/snippets/{name}` | Creates or replaces one snippet. `name` in the body is optional and must match the path. |
| `DELETE /api/v1/snippets/{name}` | Deletes one snippet (`204`). |

`/api/v1/snippet-libraries` has the same operations for libraries (without `?summary`).

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/snippet-libraries \
  -d '{"name":"hl7","description":"HL7 helpers"}'
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/snippets \
  -d '{"name":"pid","library":"hl7","code":"get(\"PID.3\")"}'
```

| Error | Cause |
|---|---|
| `400` | A bad or repeated name, an unknown field, or a body of the wrong shape. |
| `404 snippet library not found` | A snippet names a library that does not exist. Create the library first. |
| `409` | `POST` with a name that already exists, or deleting a library that snippets still name. Move or delete those snippets first. |

## Command-line client

| Command | What it does |
|---|---|
| `snippet list` | Lists snippets: name, library, description. |
| `snippet export "path"` | Writes every snippet to a JSON file. |
| `snippet import "path"` | Creates or replaces the file's snippets; others are kept. |
| `snippet remove <name>` | Deletes one snippet. |
| `snippet library list`, `export`, `import`, `remove` | The same for libraries. |

To copy snippets to another server, import the libraries before the snippets:

```text
weavster> snippet library export "libs.json"
exported 1 snippet libraries to libs.json
weavster> snippet export "snippets.json"
exported 2 snippets to snippets.json
```

and on the other server:

```text
weavster> snippet library import "libs.json"
imported 1 snippet libraries from libs.json
weavster> snippet import "snippets.json"
imported 2 snippets from snippets.json
```
