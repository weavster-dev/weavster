# Alerts

An **alert** says which processing events of which flows should notify whom. Alert definitions
are stored with the flows (durable with `store.dialect: postgres`) and managed over the API and
the command-line client. Every request needs the `alerts:edit` permission.

!!! note "Stored, not sent yet"
    Alerts do not fire yet: no email or webhook is sent. Today these endpoints store, validate,
    and return the definitions, so you can set them up and move them between servers. Testing
    an alert is a dry run that shows what would be sent.

## An alert

```json
{
  "id": "adt-errors",
  "name": "ADT errors",
  "enabled": true,
  "trigger": {"events": ["message.errored", "message.dead-lettered"], "flows": ["adt"]},
  "actions": [
    {"type": "email", "to": ["ops@example.com"]},
    {"type": "webhook", "url": "https://hooks.example.com/weavster"}
  ]
}
```

| Field | Rules |
|---|---|
| `id` | Required. 1–128 characters from `A-Z a-z 0-9 . _ -`; not `import`, `options`, or `statuses`. |
| `name` | Required, 1–200 characters. |
| `enabled` | `true` or `false` (default `false`). |
| `trigger.events` | At least one of `message.errored` (the transform failed), `message.queued` (a delivery failed and will be retried), `message.dead-lettered` (retries ran out). |
| `trigger.flows` | Flow ids. Leave it out for every flow. |
| `actions` | At least one. `{"type":"email","to":[addresses]}` with plain addresses (`ops@example.com`, not `Ops <ops@example.com>`), or `{"type":"webhook","url":"http(s)://…"}`. A webhook URL may not contain a user name or password, because everyone who can read alerts sees it; put a token in the path or query only if that is acceptable. |

Unknown fields and invalid values are rejected with `400` and a message naming the problem.
`GET /api/v1/alerts/options` lists the allowed trigger events and action types.

## API

| Request | What it does |
|---|---|
| `GET /api/v1/alerts` | Every alert, sorted by id. |
| `POST /api/v1/alerts` | Creates one alert (`201`; `409` if the id is taken). |
| `GET /api/v1/alerts/{id}` | One alert (`404` if unknown). |
| `PUT /api/v1/alerts/{id}` | Creates or replaces one alert. `id` in the body is optional and must match the path. |
| `DELETE /api/v1/alerts/{id}` | Deletes one alert (`204`). |
| `POST /api/v1/alerts/{id}/enable`, `/disable` | Turns an alert on or off and returns it. |
| `POST /api/v1/alerts/import` | Saves a JSON array of alerts, all or nothing. An id that already exists is `409` and nothing is saved; add `?force=true` to replace existing alerts. |
| `GET /api/v1/alerts/options` | `{"events": [...], "actionTypes": [...]}`. |
| `GET /api/v1/alerts/statuses` | Every alert's `id`, `name`, and `enabled`, sorted by id. |
| `GET /api/v1/alerts/{id}/info` | `{"alert": {...}, "events": [...], "actionTypes": [...]}`: the alert with the values it may use (`404` if unknown). |
| `POST /api/v1/alerts/{id}/test` | A dry run: whether an event of a flow would trigger the alert, and which actions would run. See below. |

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/alerts \
  -d '{"id":"adt-errors","name":"ADT errors","enabled":true,"trigger":{"events":["message.errored"],"flows":["adt"]},"actions":[{"type":"email","to":["ops@example.com"]}]}'
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/alerts/adt-errors/disable
```

## Test an alert

`POST /api/v1/alerts/{id}/test` checks an alert against an event without waiting for one. The body
is optional: `event` defaults to the alert's first trigger event and `flowId` to its first flow.
A disabled alert can be tested too; `enabled` shows its state.

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST \
  http://127.0.0.1:8080/api/v1/alerts/adt-errors/test -d '{"event":"message.errored","flowId":"adt"}'
```

```json
{"matches":true,"enabled":true,"event":"message.errored","flowId":"adt","actions":[{"type":"email","to":["ops@example.com"]}],"delivered":false}
```

- `matches` is false, with no `actions`, when the event is not in `trigger.events` or the flow
  is not in `trigger.flows`.
- `delivered` is always `false`: nothing is sent, not even a test message.
- An event that is not one of the trigger events in `GET /api/v1/alerts/options`, or an
  invalid flow id, returns `400`.

## Command-line client

| Command | What it does |
|---|---|
| `exportalert id "path"` | Writes one alert, chosen by id or name (quote a name with spaces), to a JSON file. An id match wins; a name that several alerts share is refused, so export those by id. |
| `exportalert * "path"` | Writes every alert. |
| `importalert "path" [force]` | Saves the alerts in the file. Without `force`, an alert whose id already exists stops the import and nothing is saved. |

```text
weavster> exportalert * "alerts.json"
exported 2 alerts to alerts.json
weavster> importalert "alerts.json" force
imported 2 alerts from alerts.json
```

The file is a JSON array of alerts, the same shape the API uses, so you can edit it before
importing it on another server.
