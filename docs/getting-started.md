# Getting started

In about ten minutes, run a server on your machine, test a flow offline, apply it from a file,
and send it a message that is transformed and delivered. You need Docker (with Compose) and Go
1.22 or later, and a checkout of the repository:

```bash
git clone https://github.com/weavster-dev/weavster
cd weavster
```

## 1. Start the server

```bash
docker compose up -d --build --wait
until curl -fsS http://127.0.0.1:8080/api/openapi.yaml >/dev/null; do sleep 1; done
```

`--wait` returns once the containers run; the second line waits until the server answers (on
the first start it migrates the database first, a few seconds). This runs the server on `http://127.0.0.1:8080` with PostgreSQL 16 behind it, and an `admin`
user with the development password `Weavster-dev-1`. See
[Docker Compose](docker-compose.md) for what it sets up; for a real deployment, see
[Production setup](production.md).

## 2. Build the command-line client

```bash
go build -o bin/weavster ./cmd/weavster
```

(Or [install a release](install.md).) The same binary is the server, the client, and the test
runner.

## 3. Describe a flow in a file

A flow is kept in a [config-as-code](config-as-code.md) document in your own repository.
`examples/getting-started/hello.yaml` has one flow, `hello`: it reads a JSON message, copies two
fields into a greeting, and writes the result to a file.

```yaml
# A config-as-code document with one flow: it reads a JSON message, copies
# two fields into a greeting, and writes the result to a file.
version: "1"
flows:
  hello:
    name: Hello
    transform:
      name: greet
      steps:
        - map: {from: patient.name, to: greeting.to}
        - map: {from: patient.mrn, to: greeting.mrn}
    destinations:
      - name: out
        type: file
        dir: /tmp/hello
```

## 4. Test it offline

Next to it, `hello.test.yaml` says what the flow must make of a sample message:

```yaml
# What the hello flow must make of a message, checked offline by
# `weavster test` before anything reaches a server.
flow: hello
cases:
  - name: greets the patient
    input: '{"patient":{"name":"Ada Lovelace","mrn":"12345"}}'
    expect:
      output: {greeting: {to: Ada Lovelace, mrn: "12345"}}
```

`weavster test` runs it through the flow's transform on your machine, with no server:

```bash
bin/weavster test examples/getting-started
```

It prints JUnit XML with every case passing and exits `0`. Change `Ada Lovelace` in the
expectation and run it again to see a failure explained. See
[Test your flows](cli.md#test-your-flows-weavster-test).

## 5. Apply the file and start the flow

`examples/getting-started/setup.txt` is a short script for the command-line client: it applies
the document (the words after the path are the reason, recorded in the audit log), then deploys
and starts the new flow, which `config apply` creates undeployed.

```text
config apply "examples/getting-started/hello.yaml" first flow
flow deploy hello
flow start hello
flow list
```

```bash
bin/weavster -a http://127.0.0.1:8080 -u admin -p Weavster-dev-1 -s examples/getting-started/setup.txt
```

The client prints the plan and what it applied, the flow after each lifecycle step, and the
flow list:

```text
+ flow/hello
1 to add, 0 to change, 0 to remove, 0 unchanged
applied 1 changes
{"id":"hello","name":"Hello",…,"status":"deployed",…}
{"id":"hello","name":"Hello",…,"status":"started",…}
hello	started	Hello
```

## 6. Send a message

```bash
curl -s -u admin:Weavster-dev-1 -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/flows/hello/messages -d '{"patient":{"name":"Ada Lovelace","mrn":"12345"}}'
```

```json
{"id":"1ef8fb26d76f9643c3d95db0a90bb97f","status":"sent"}
```

`sent` means the message was stored, transformed, and written to the file destination: a file in
`/tmp/hello` **inside the server's container** (not on your machine). To look at it, copy the
directory out with `docker compose cp weavster:/tmp/hello ./hello-out` (the image has no shell,
so `docker compose exec` does not work). Or read what the transform made of the message with
the id from the reply:

```bash
curl -s -u admin:Weavster-dev-1 -H 'X-Weavster-CSRF: 1' 'http://127.0.0.1:8080/api/v1/messages/1ef8fb26d76f9643c3d95db0a90bb97f/content?part=transformed'
```

```json
{"greeting":{"mrn":"12345","to":"Ada Lovelace"},"patient":{"mrn":"12345","name":"Ada Lovelace"}}
```

Open `http://127.0.0.1:8080/ui/` and sign in as `admin` to see the flow, its destination, and
the message count in the [web UI](web-ui.md).

## Next steps

- [Processing messages](processing-messages.md): sources (files, HTTP, MLLP, databases),
  transforms, destinations, retries, and searching messages.
- [Config-as-code](config-as-code.md) and [CI/CD](ci-cd.md): plan and apply from your repository.
- [Authentication](authentication.md): users and permissions.

## Start over

```bash
docker compose down -v
```

This removes the containers and the database volume, so the next `docker compose up` starts
empty. Running `setup.txt` a second time without that fails at `flow deploy`, because the flow
is already started.
