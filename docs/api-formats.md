# JSON and XML

The API speaks JSON. A client that prefers XML gets every JSON response as XML instead, errors
included.

## Ask for XML

Send `Accept: application/xml` (or `text/xml`):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' -H 'Accept: application/xml' \
  http://127.0.0.1:8080/api/v1/flows/adt
```

```xml
<?xml version="1.0" encoding="UTF-8"?>
<response><id>adt</id><name>ADT Inbound</name><sourceType></sourceType><status>started</status><enabled type="boolean">true</enabled><destinations type="array"><item><name>out</name><type>file</type><dir>/var/out</dir></item></destinations></response>
```

XML is used when the `Accept` header ranks an XML type above JSON. Without an `Accept` header,
with `*/*`, or when JSON ranks at least as high, the reply is JSON. For example,
`application/json;q=0.5, application/xml` gets XML, and `application/json, application/xml`
gets JSON. Replies carry `Vary: Accept`.

## How JSON becomes XML

| JSON | XML |
|---|---|
| The whole reply | `<response>` |
| An object | Its keys as child elements, in the same order: `{"id":"adt"}` → `<id>adt</id>`. |
| A key that is not an XML name (`ICD/10`, `1st`, one starting with `xml`) | `<entry key="ICD/10">…</entry>` |
| An array | `type="array"`, with one `<item>` per element. |
| A string | Text (`<`, `&`, and quotes escaped). |
| A number, `true`/`false`, `null` | Text with `type="number"`, `type="boolean"`, or an empty element with `type="null"`. Numbers keep every digit. |

An error is the same envelope as in JSON:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<response><error><code>NOT_FOUND</code><message>flow not found</message></error></response>
```

## What stays as it is

- **Request bodies are JSON only** (and YAML for [config-as-code documents](config-as-code.md)).
  Send `Content-Type: application/json`; an XML body is refused as invalid JSON.
- Replies that are not JSON are never converted: message archives (`application/gzip` or
  encrypted), the OpenAPI document (`GET /api/openapi.yaml`), and message content from
  `/api/v1/messages/{id}/content`.
