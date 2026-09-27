# REST API

The server's REST API lives under `/api`. Every request needs credentials (see
[Authentication](authentication.md)) except login (`POST /api/v1/auth/login`, or its unversioned
form `POST /api/auth/login`) and `GET /api/openapi.yaml`. Every request except
`GET /api/openapi.yaml`, versioned or unversioned, needs the header `X-Weavster-CSRF: 1` unless
`listen.requireMarkerHeader` is off.

## Versions

Each endpoint has a versioned path and an unversioned one:

| Path | Serves |
|---|---|
| `/api/v1/flows` | Version 1. Use this in scripts and integrations: its behaviour will not change when a newer version appears. |
| `/api/flows` | The latest version, today v1. |

Both paths behave identically, including errors, and the [audit log](audit-log.md) records the
path you called. Every reply from a versioned or unversioned endpoint path carries the version that
answered it (`GET /api/openapi.yaml` is not versioned and has no such header):

```bash
curl -si -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/flows | grep -i weavster-api-version
```

```text
Weavster-API-Version: v1
```

A version that does not exist, such as `/api/v9/flows`, returns `404`.

## Contract

The full contract is served without credentials at `GET /api/openapi.yaml` (OpenAPI 3.1). It is the
same file as `agent-docs/openapi.yaml` in the repository and documents every endpoint with its
`/api/v1` path.

## Errors

Every error has the same JSON shape. See [API errors](api-errors.md).
