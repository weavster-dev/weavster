# @weavster/cli

The Weavster command-line tool: scaffold, validate, and test config-driven data
transformation projects. You describe transformations as YAML flows over a canonical
document model, and run them locally against fixtures.

## Install

```bash
npm install -g @weavster/cli
```

## Quickstart

```bash
weavster init my-integration   # scaffold a project
cd my-integration
weavster validate              # check weavster.yaml + flows against their schemas
weavster test                  # run fixtures through flows, diff against expected output
```

A scaffolded project looks like:

```text
my-integration/
  weavster.yaml          # project config (apiVersion + name)
  flows/main.yaml        # a transform pipeline
  fixtures/main/basic/   # input.json + expected.json
  README.md
```

## Commands

- `weavster init [dir]` — scaffold a new project that passes `weavster test` out of the box.
- `weavster validate [path]` — validate `weavster.yaml` and every `flows/*.yaml`.
- `weavster test [path]` — run each fixture through its flow and report a diff on mismatch.

## Flows in brief

Flows are a patch-by-default pipeline of single-key `_op` steps; values are expressions with
`$path` references and `_op` operators:

```yaml
steps:
  - _set:
      id: { _upper: $id }
      name: { _concat: { parts: [$first, $last], sep: ' ' } }
  - _when:
      cond: { _eq: [$status, new] }
      then:
        - _set: { priority: high }
```

When the declarative DSL isn't enough, a `_ts` step runs a custom TypeScript function under a
pure JSON-in/JSON-out contract.

## Documentation

Full docs — concepts, the transform DSL, format packs, and the TypeScript escape hatch — are
at [docs.weavster.dev](https://docs.weavster.dev).

## License

[BUSL-1.1](https://github.com/weavster-dev/weavster/blob/main/LICENSE).
