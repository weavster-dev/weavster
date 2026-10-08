# Metrics

The server publishes Prometheus metrics at `GET /metrics` on its API listener (`listen.address`
and/or `listen.tlsAddress`). The numbers are the server's own statistics, read when Prometheus
scrapes, so they always match `GET /api/v1/flows/{id}/stats?lifetime=true`.

`/metrics` needs credentials of a user with `flows:view`. It does not need the
`X-Weavster-CSRF` header, so a plain Prometheus scrape works:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: weavster
    scheme: https
    static_configs:
      - targets: ["weavster.internal:8443"]
    basic_auth:
      username: prometheus
      password_file: /etc/prometheus/weavster-password
```

Create a user with only `flows:view` for Prometheus, and send `"mustChangePassword": false` so
it can sign in with the password you gave it (otherwise every scrape gets
`403 PASSWORD_CHANGE_REQUIRED` until the password is changed; see
[Authentication](authentication.md#manage-users)):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -X POST http://127.0.0.1:8080/api/v1/users \
  -d '{"username":"prometheus","password":"A-Long-Scrape-Pass1","permissions":["flows:view"],"mustChangePassword":false}'
```

A scrape without credentials gets `401` and is recorded in the [audit log](audit-log.md) as
`auth.failure`.

```bash
curl -s -u 'prometheus:PASSWORD' https://weavster.internal:8443/metrics | grep '^weavster_'
```

```text
weavster_connector_messages_total{connector="ehr",flow="adt",outcome="errored"} 1
weavster_connector_messages_total{connector="ehr",flow="adt",outcome="sent"} 120
weavster_flow_messages_total{flow="adt",outcome="received"} 121
weavster_flow_messages_total{flow="adt",outcome="sent"} 120
weavster_flows{status="started"} 3
weavster_processing_in_flight 0
weavster_processing_refused_total 0
weavster_processing_slots 32
```

## What is published

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `weavster_flow_messages_total` | counter | `flow`, `outcome` | A flow's messages by outcome: `received`, `filtered`, `transformed`, `sent`, `queued`, `errored`. |
| `weavster_connector_messages_total` | counter | `flow`, `connector`, `outcome` | Deliveries to a flow's destination (`connector`, the destination's name): `sent`, or `errored` for every failed attempt (retries included). |
| `weavster_flows` | gauge | `status` | Flows in each lifecycle status (`started`, `stopped`, `undeployed`, …). |
| `weavster_processing_in_flight` | gauge | | Messages from the API and flow sources being received and processed right now. Retries of queued messages and hand-offs from one flow to another take no slot and are not counted. |
| `weavster_processing_slots` | gauge | | How many can be at once (`processing.maxConcurrent`). |
| `weavster_processing_refused_total` | counter | | Messages refused as busy because every slot was taken. |
| `go_*`, `process_*` | | | The Go runtime and the process (memory, goroutines, open files; which `process_*` metrics exist depends on the operating system). |

The counters are the lifetime statistics. Every flow is published, with zeros before its first
message; a deleted flow's series go away. The counters start at zero when the server starts and go
back to zero when you reset them (`POST /api/v1/flows/stats/reset?lifetime=true`); Prometheus
treats that as a counter reset, so `rate()` and `increase()` stay correct.

A message counts `queued` once, when its first delivery fails, and `sent` once it is delivered;
each failed attempt in between counts in `weavster_connector_messages_total{outcome="errored"}`.
Watch that, not `queued`, for a destination that keeps failing. Without a message store
(`store.dialect: disabled`) the `weavster_processing_*` metrics are not published. Each scrape
reads the flow list from the store; if that fails, the flow metrics are left out of that scrape
and the server logs `metrics: flows could not be listed`.
