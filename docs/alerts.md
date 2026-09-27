# Alerts

An **alert** says which processing events of which flows should notify whom. Alert definitions
are stored with the flows (durable with `store.dialect: sqlite`) and managed over the API and
the command-line client. Every request needs the `alerts:edit` permission.

!!! note "Stored, not sent yet"
    Alerts do not fire yet: no email or webhook is sent. Today these endpoints store, validate,
    and return the definitions, so you can set them up and move them between servers.

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
| `id` | Required. 1–128 characters from `A-Z a-z 0-9 . _ -`; not `import` or `options`. |
| `name` | Required, 1–200 characters. |
| `enabled` | `true` or `false` (default `false`). |
| `trigger.events` | At least one of `message.errored` (the transform failed), `message.queued` (a delivery failed and will be retried), `message.dead-lettered` (retries ran out). |
| `trigger.flows` | Flow ids. Leave it out for every flow. |
| `actions` | At least one. `{"type":"email","to":[addresses]}` or `{"type":"webhook","url":"http(s)://…"}`. |

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

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/alerts \
  -d '{"id":"adt-errors","name":"ADT errors","enabled":true,"trigger":{"events":["message.errored"],"flows":["adt"]},"actions":[{"type":"email","to":["ops@example.com"]}]}'
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/alerts/adt-errors/disable
```

## Command-line client

| Command | What it does |
|---|---|
| `exportalert id "path"` | Writes one alert, chosen by id or name (quote a name with spaces), to a JSON file. |
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
