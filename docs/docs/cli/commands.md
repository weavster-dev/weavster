---
sidebar_position: 1
---

# Commands

## weavster init

Initialize a new Weavster project.

```bash
weavster init <name>
```

**Arguments:**
- `<name>` - Project directory name

**Example:**
```bash
weavster init my-project
```

## weavster run

Run the project locally.

```bash
weavster run [OPTIONS]
```

**Options:**
- `--flow <name>` - Run a specific flow only
- `--profile <name>` - Use a specific profile
- `--once` - Process once and exit (no continuous polling)

**Examples:**
```bash
# Run all flows
weavster run

# Run specific flow
weavster run --flow my-flow

# Run with production profile
weavster run --profile production
```

## weavster compile

Compile flows to WASM artifacts.

```bash
weavster compile [OPTIONS]
```

**Options:**
- `--flow <name>` - Compile specific flow
- `--debug` - Output generated Rust code for inspection

**Examples:**
```bash
# Compile all flows
weavster compile

# Compile with debug output
weavster compile --debug
```

## weavster validate

Validate configuration without running.

```bash
weavster validate
```

## weavster package

Create OCI artifact for distribution.

```bash
weavster package [OPTIONS]
```

**Options:**
- `--sign` - Sign with cosign

## weavster push

Push OCI artifact to registry.

```bash
weavster push <registry>
```

## weavster pull

Pull OCI artifact from registry.

```bash
weavster pull <registry>
```

## Global Options

Available on all commands:

- `--help` - Show help
- `--version` - Show version
- `--verbose` / `-v` - Increase verbosity
- `--quiet` / `-q` - Suppress output
