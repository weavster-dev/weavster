---
sidebar_position: 1
---

# Commands

The CLI currently exposes implemented, partial, placeholder, and planned surfaces. Run `weavster --help` for the command list supported by your local binary.

## Status Summary

| Command | Status | Notes |
| --- | --- | --- |
| `weavster init` | Current | Creates a starter project |
| `weavster run` | Current | Runs the file-based flow path, with limited option semantics |
| `weavster compile` | Current | Compiles flows to WASM artifacts |
| `weavster test` | Partial | Runs YAML-defined flow tests through compiled WASM where available |
| `weavster validate` | Partial | Loads project config; deeper validation is still TODO |
| `weavster package` | Partial | Creates an artifact directory; digest/signing are incomplete |
| `weavster status` | Placeholder | Prints "Not yet implemented" |
| `weavster flow` | Placeholder | Subcommands exist but are not implemented |
| `weavster connector` | Placeholder | Subcommands exist but are not implemented |
| `weavster push` / `weavster pull` | Planned | Not current CLI commands |

## Global Options

```bash
weavster [OPTIONS] <COMMAND>
```

| Option | Status | Notes |
| --- | --- | --- |
| `-c, --config <CONFIG>` | Current | Defaults to `weavster.yaml` |
| `-v, --verbose` | Current | Enables verbose logging |
| `-h, --help` | Current | Prints help |
| `-V, --version` | Current | Prints version |

There is no current `--quiet` global option.

## `weavster init`

Initialize a new project.

```bash
weavster init my-project
weavster init . --name my-project
```

Creates `weavster.yaml`, `flows/example_flow.yaml`, `connectors/file.yaml`, `tests/`, and sample JSONL input data.

## `weavster run`

Run the runtime.

```bash
weavster run --once
weavster run --profile production
weavster run --flow example_flow
```

Options:

| Option | Status | Notes |
| --- | --- | --- |
| `--once` | Partial | Accepted by the CLI; current file flow already runs over available input records and exits |
| `--flow <name>` | Partial | Accepted by the CLI; flow filtering semantics are limited |
| `--profile <name>` | Current | Loads an inline profile from `weavster.yaml` |

## `weavster compile`

Compile flows to WASM artifacts.

```bash
weavster compile
weavster compile --flow example_flow
weavster compile --debug
weavster compile --force
```

Options:

| Option | Status | Notes |
| --- | --- | --- |
| `--flow <name>` | Current | Compiles one flow |
| `--debug` | Current | Saves generated Rust for inspection |
| `--force` | Current | Ignores cached artifacts |
| `--profile <name>` | Current | Uses a profile during config loading |

## `weavster validate`

Load and parse project configuration.

```bash
weavster validate
weavster validate --profile production
```

This command is partial. It loads config successfully, but deeper validation of flow files, connector references, and transform expressions is still TODO.

## `weavster package`

Create a local artifact directory from compiled flows and config.

```bash
weavster compile
weavster package --output .weavster/artifact
```

This command is partial. The manifest digest is currently a placeholder, and `--sign` only checks for cosign before warning that signing is not fully implemented.

## `weavster test`

Run YAML-defined tests from a project `tests/` directory.

```bash
weavster test
weavster test example
weavster test --profile development
```

Test execution is partial and centered on compiled WASM flow checks.

## Placeholder Commands

These commands exist in the CLI but currently print placeholder output:

```bash
weavster status
weavster flow list
weavster flow show <name>
weavster flow new <name>
weavster connector list
weavster connector test <name>
```
