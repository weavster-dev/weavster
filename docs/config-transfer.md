# Export and import the whole configuration

One document holds a server's whole configuration: flows, [alerts](alerts.md),
[code snippets and libraries](snippets.md), [global scripts and settings](config-items.md), and,
when you ask for it, the config map. Use it to back up a server or to copy its setup to another
one. Users and passwords are never included; create them again on the other server.

## Export

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/config/export?includeConfigMap=true' > config.json
```

```json
{"format":"weavster-config-v1",
 "flows":[{"id":"adt","enabled":true,"destinations":[{"name":"ehr","type":"http","url":"https://ehr.example.com/in"}]}],
 "alerts":[],"snippets":[],"snippetLibraries":[],
 "scripts":{"deploy":"log('deployed')"},"settings":{"retention":{"days":30}},
 "configmap":{"region":"eu"}}
```

Flows are exported without their runtime state. The config map is left out unless you add
`includeConfigMap=true`, because it usually holds values for one environment (hostnames, for
example).

## Import

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST \
  'http://127.0.0.1:8080/api/v1/config/import' --data-binary @config.json
```

```json
{"flows":{"created":["adt"],"updated":[]},"alerts":0,"snippets":0,"snippetLibraries":0,
 "scripts":1,"settings":1,"configMapReplaced":false,"deployed":["adt"]}
```

| Parameter | What it does |
|---|---|
| `force=true` | Replaces flows, alerts, snippets, and libraries that already exist. Without it, any that exist stop the import with `409` naming them, and nothing is written. |
| `nodeploy=true` | Leaves the imported flows undeployed. Without it, every imported flow that is enabled and undeployed is deployed afterwards and listed in `deployed`. |
| `overwriteConfigMap=true` | Replaces the config map with the document's. Without it, the server keeps its own config map and the document's is ignored. |

- Everything is checked before anything is written: the format, every flow, alert, snippet, and
  value, and that each snippet's library is in the document or already on the server. Problems
  return `400` naming the item.
- Global scripts and settings in the document are created or replaced; others on the server are
  kept.
- If writing fails part-way (for example, the database is unavailable), the reply is `500`. Fix
  the cause and import again with `force=true`.
- If a flow does not deploy, the reply is `500` and names it; the configuration is imported.
  Deploy the flow yourself with `POST /api/v1/flows/{id}/deploy`.

## Permissions

Export needs `flows:view`, `alerts:edit`, `snippets:edit`, `scripts:edit`, `settings:edit`,
and `configmap:edit`. Import needs `flows:edit`, `flows:deploy`, and the same `…:edit`
permissions. The `admin` permission includes them all.

## Command-line client

| Command | What it does |
|---|---|
| `exportcfg "path"` | Writes the configuration without the config map to a file. |
| `exportcfg "path" overwriteconfigmap` | Also includes the config map, so importing with `overwriteconfigmap` replaces the target's config map. |
| `importcfg "path" [nodeploy] [overwriteconfigmap] [force]` | Imports a file written by `exportcfg`. The options match the parameters above and can come in any order. |

```text
weavster> exportcfg "config.json" overwriteconfigmap
exported 2 flows, 1 alerts, 1 snippets, 1 snippet libraries, 1 scripts, 1 settings, 1 config map entries to config.json
weavster> importcfg "config.json" overwriteconfigmap
imported 2 flows (2 new, 0 replaced), 1 alerts, 1 snippets, 1 snippet libraries, 1 scripts, 1 settings from config.json
replaced the config map
deployed adt
```

A file that does not exist is reported (`Error: open config.json: no such file or directory`)
and nothing is sent to the server.
