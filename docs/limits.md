# Capacity and limits

Every limit the server applies, its default, and what happens when it is reached. Limits you can
change are marked with their configuration key or flow field.

## Messages

| What | Limit | At the limit |
|---|---|---|
| Message body (API, every source) | 10 MiB | API and http source `413`; mllp source `AR`; file source moves the file to `moveTo/rejected` (or skips it); database source reports the row as `source.database.refused` and leaves it unmarked. |
| Messages processed at once | 32 — `processing.maxConcurrent` (1–10000) | A new message waits up to `processing.waitMs` (default 5000, up to 60000), then is refused as busy: API/http `503` + `Retry-After: 1`, mllp `AE`, file and database sources try again a moment later. See [`processing`](server-config.md#processing). |
| Delivery attempts per destination | 5 — `delivery.maxAttempts` (1–1000) | The message is `dead-lettered`; requeue it when the destination is back. |
| Retry delay | 1 s — `delivery.backoffBaseMs`, doubling per attempt, at most 1 minute | — |
| HTTP destination reply | 1 MiB read | Larger replies still count as delivered; the reply is not stored or used. |
| Destination timeout | 30 s — `timeoutMs` per http, mllp, or database destination (1000–120000) | The attempt fails with `lastCode` `net:timeout` and is retried. |

## Sources

| What | Limit | At the limit |
|---|---|---|
| http source request read | 60 s — `readTimeoutMs` (1000–600000); headers within 10 s | The connection is closed. |
| mllp source connection idle | 5 minutes | The connection is closed; the sender reconnects. |
| mllp source frame | 15 minutes to arrive once started; 10 MiB | Larger frames are read to their end and answered `AR`. |
| mllp TLS handshake | 30 s | The connection is closed. |
| mllp destination ACK | 1 MiB | `mllp:ack-too-large`; retried. |
| File source files per poll | 100 | The rest are read in back-to-back polls. |
| File source recursion | 32 directory levels | Deeper directories are not read. |
| File source settle time | 1 s unchanged before a file is read | The file is read once it stops changing. |
| File source interval | 1 s — `pollIntervalMs` (100–3600000), or a cron `schedule` | — |
| Database source rows per poll | 100 — `maxRows` (1–10000), and at most 64 MiB of row data | The rest are read in back-to-back polls. |
| Database source interval | 5 s — `pollIntervalMs` (1000–3600000), or a cron `schedule` | — |
| Database query or update | 30 s — `timeoutMs` (1000–120000) | The poll fails (`source.database.failed`) and is retried. |

## Formats

| What | Limit | At the limit |
|---|---|---|
| XML nesting / elements | 256 levels / 100,000 elements | The message is refused as not well-formed XML (`400`, `AR`). |
| Delimited text | 100,000 rows / 1,000,000 values | The message is refused as not valid delimited text. |
| HL7 v2 versions | 2.1–2.9 (with `inputFormat: hl7v2`) | The message is refused (`400`, `AR`). |

## API and CLI

| What | Limit |
|---|---|
| Message search page | `limit` 1–1000 (default 100); use `offset` for the next page. |
| Message archive export / import | 10,000 messages per export; imports up to 100 MiB (512 MiB uncompressed). |
| Flow import, bulk update, config documents | 50 MiB per request or file. |
| Config map, scripts, settings bodies | 10 MiB. |
| Events per request | 1000 by default. |
| Statistics series | 1000 points per request by default; `limit` up to 10000. |
| Message trends | 1000 buckets per request. |
| `weavster deadletter list` | 1000 messages per call. |

## Retention

| Data | Where | Kept |
|---|---|---|
| Messages, their content, metadata, and attempt records | The store (`store.dialect`) | Until you remove them (`DELETE /api/v1/messages/{id}`, or [many at once](processing-messages.md#remove-many-messages)). Nothing is pruned automatically: size your database for your traffic, and remove old messages on a schedule of your own, for example with a nightly `DELETE /api/v1/messages?to=…`. With `memory`, until the server stops. |
| Flow definitions, users, config items | The store | Until changed or removed. |
| Events | Server memory | The latest 10,000; lost on restart. |
| Statistics (current, lifetime) | Server memory | Until reset or restart. |
| Statistics over time | Server memory | `stats.retentionHours` (default 24, up to 8760), at most 100,000 samples per flow and 1,000,000 in all. |
| Audit log | Server stderr | As long as your log collection keeps it. |

To estimate the store's size, count each message's body several times: the original, the stored
received form, the transformed output, and a reply when a destination returns one.
