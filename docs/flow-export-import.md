# Flow dependencies, export, and import

## Dependencies

A flow can declare the flows it needs with `dependsOn` (a list of flow ids):

```json
{"id": "adt-archive", "name": "ADT archive", "dependsOn": ["adt"]}
```

The server checks dependencies on create, update, and import. It returns `400` when:

| Problem | Example message |
|---|---|
| A listed flow does not exist | `flow adt-archive depends on unknown flow adt` |
| A flow lists itself | `flow adt cannot depend on itself` |
| The dependencies form a loop | `dependency cycle: a -> b -> a` |

You cannot delete a flow that another flow depends on:

```text
409 {"error":{"code":"CONFLICT","message":"flow is a dependency of other flows: adt depended on by adt-archive"}}
```

To prove nothing depends on it, the server must be able to read every other flow. If one is
unreadable, the delete returns `500` naming that flow; delete the unreadable flow first (a flow
can always delete itself).

Deploying a flow, manually or through startup auto-deploy, also deploys every `undeployed` flow
it depends on, dependencies first. Dependencies in any other status are left as they are, so
deploying a dependent never interrupts a running dependency. Starting a flow does not start its
dependencies. See [Flow lifecycle](flow-lifecycle.md).

## Export

`GET /api/v1/flows/export` (permission `flows:view`) returns flow definitions:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' \
  'http://127.0.0.1:8080/api/v1/flows/export?ids=adt-archive' > flows.json
```

```json
{"version":1,"flows":[
  {"id":"adt","name":"ADT normalize","sourceType":"http","enabled":true,"transform":{…},"destinations":[…]},
  {"id":"adt-archive","name":"ADT archive","sourceType":"","enabled":false,"dependsOn":["adt"]}
]}
```

- `ids` is a comma-separated list. The export also includes every flow those flows depend on,
  directly or indirectly. Leave `ids` out to export all flows.
- Flows are sorted by id and have no `status`: an export holds definitions, not runtime state.
- An unknown id returns `404`.

## Import

`POST /api/v1/flows/import` (permission `flows:edit`) takes an export document:

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST \
  http://127.0.0.1:8080/api/v1/flows/import --data @flows.json
```

```json
{"created":["adt","adt-archive"],"updated":[]}
```

The whole document is checked before anything is written. Every flow must be valid, the ids
must be unique, and dependencies must resolve against the document plus the existing flows,
without cycles. Otherwise the import returns `400` and changes nothing. Flows are then written
dependencies first. Each flow in the document is a full definition: a flow without `enabled`
is imported disabled. Exports always include `enabled`.

| Flow in the document | Result |
|---|---|
| New id | Created `undeployed`. Deploy and start it to process messages. |
| Existing id, without `?overwrite=true` | Nothing is written: `409 flows already exist: adt (use overwrite=true to replace them)`. Checked after the document is validated, so an invalid document returns `400` even when it also conflicts. |
| Existing id, with `?overwrite=true` | Definition replaced; the flow keeps its current status. |

Other errors:

| Response | Cause |
|---|---|
| `400 unsupported export version 2; expected 1` | Missing or unknown `version`. |
| `400 flows[0]: status is managed by lifecycle operations …; omit it` | A flow in the document has a `status` field. |
| `400 flows[0]: stoppedDestinations is managed by …; omit it` | A flow in the document has a `stoppedDestinations` field. |
| `400 … appears twice` | The same id is listed twice. |
| `400 body must be an export document` | Not a JSON object, no `flows` array, a top-level field other than `version` and `flows`, or extra data after the document. |
| `413` | The document is larger than 50 MiB. |

If writing fails part-way (for example, a store error), the import stops and returns
`500 {"error":{"code":"IMPORT_INCOMPLETE",…},"created":[…],"updated":[…]}`, listing what was
written. Because flows are written dependencies first, every written flow's dependencies exist.
Import again with `?overwrite=true` to finish.

Creates, updates, imports, and deletes of flow definitions run one at a time, so a dependency
check always sees the flows it is written against.

## Reserved ids

`export`, `import`, `redeploy-all`, `connector-names`, `ports-in-use`, `stats`, and `deploy-all`, `undeploy-all`, `start-all`, `stop-all`, `pause-all`, `halt-all`, `resume-all` cannot be used as flow ids, because they name these
endpoints. A flow created with one of these ids before this rule existed can still be deleted
(`DELETE /api/v1/flows/export`). To keep it, export all flows, change its id in the file,
delete it, and import the file.
