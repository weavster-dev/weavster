# System information

Any signed-in user can ask the server about itself. Nothing here changes anything.

## Status

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/system
```

```json
{"id":"weavster","status":"running","version":"0.1.0","buildDate":"2026-09-27",
 "time":"2026-09-27T12:00:00-04:00","timezone":"EDT -04:00","uptimeSeconds":3600,"runtime":"go1.25.4",
 "charsets":["UTF-8","ISO-8859-1"],
 "tls":{"enabled":true,"address":"0.0.0.0:8443","minVersion":"1.3","protocols":["TLS 1.3"],
        "ciphers":["TLS_AES_128_GCM_SHA256","TLS_AES_256_GCM_SHA384","TLS_CHACHA20_POLY1305_SHA256"]},
 "license":"MVP (no entitlement gating)"}
```

- `time` and `uptimeSeconds` are computed for each request; `timezone` is the server's zone
  abbreviation and offset.
- `tls` describes the HTTPS listener as configured (`listen.tlsAddress`, `tls.minVersion`; see
  [Server configuration](server-config.md)). Without an HTTPS listener it is
  `{"enabled":false,"protocols":[],"ciphers":[]}`. With `minVersion: "1.2"` the list also has
  TLS 1.2 and the ECDHE AES-GCM suites that match the certificate's key (RSA, or ECDSA for ECDSA and Ed25519 keys).

## Other requests

| Request | What it returns |
|---|---|
| `GET /api/v1/system/about` | `name`, `version`, `buildDate`, `runtime`, `os`, `arch`, `license`. |
| `GET /api/v1/system/password-requirements` | The [password policy](server-config.md#auth): `minLength`, `minUpper`, `minLower`, `minNumeric`, `minSpecial` (`0` = no requirement, `-1` = not allowed), and `rules`, the same in words for showing to users. |
| `GET /api/v1/system/resources` | `cpus`, `goroutines`, `memoryAllocBytes`, `memorySysBytes`, `uptimeSeconds` of the server process. |
| `GET /api/v1/system/guid` | `{"guid": "…"}`: a new random UUID each time. |

```json
{"minLength":12,"minUpper":1,"minLower":1,"minNumeric":1,"minSpecial":-1,
 "rules":["at least 12 characters","at least 1 uppercase letter","at least 1 lowercase letter","at least 1 digit","no special characters (only letters and digits)"]}
```

If the server was built without system information (only in embedded test setups), every
request except `/guid` returns `503`.
