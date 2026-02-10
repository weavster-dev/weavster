# MVP: File Transform Pipeline

## Objective

`weavster init . && weavster run --once` reads a JSONL input file, applies transforms (map, drop, add_fields), and writes a JSONL output file. No database. No WASM. No job queue.

## Outcome

```bash
$ mkdir /tmp/weavster-test && cd /tmp/weavster-test
$ weavster init .
$ weavster run --once
Processing flow: example_flow
  Read 3 messages from data/input.jsonl
  Wrote 3 messages to data/output.jsonl
Flow example_flow completed: 3 processed, 0 failed

$ cat data/output.jsonl
{"full_name":"Alice Johnson","email":"alice@example.com","processed":true}
{"full_name":"Bob Smith","email":"bob@example.com","processed":true}
{"full_name":"Carol Williams","email":"carol@example.com","processed":true}
```

## Pre-conditions

- `cargo build` passes
- `cargo test` passes (132 existing tests)
- `cargo clippy -- -D warnings` clean

## Steps

Each step is a discrete change. Verify the check after each step before proceeding.

---

### 1. Implement JSONL file reading in FileInputConnector

**Files**: `crates/weavster-core/src/connectors.rs`

Replace the `FileInputConnector` stub. The `pull()` method should:
- On first call, open the file at `self.config.path` using `BufReader`
- Read one line per call, parse as `serde_json::Value`
- Return `Ok(Some(Message { payload, metadata }))` with line number as metadata id
- Return `Ok(None)` at EOF
- Store the reader as `Option<BufReader<File>>` (lazy init)

The `ack()` and `nack()` methods remain no-ops for files.

Remove `#[allow(dead_code)]` from the struct since fields are now used.

**Check**: `cargo test -p weavster-core` passes. Add a unit test that reads a fixture JSONL string via `Cursor<&[u8]>` or a tempfile.

---

### 2. Implement JSONL file writing in FileOutputConnector

**Files**: `crates/weavster-core/src/connectors.rs`

Replace the `FileOutputConnector` stub. The `push()` method should:
- On first call, create/open the output file, create `BufWriter`
- Serialize `message.payload` to JSON string, write as one line
- Store the writer as `Option<BufWriter<File>>`

The `flush()` method should flush the underlying writer.

Remove `#[allow(dead_code)]` from the struct.

**Check**: `cargo test -p weavster-core` passes. Add a unit test that writes messages to a tempfile and verifies contents.

---

### 3. Create the transform interpreter

**Files**: `crates/weavster-core/src/interpreter.rs` (new), `crates/weavster-core/src/lib.rs`

Create a new module with a single public function:

```rust
pub fn apply_transforms(
    input: &serde_json::Value,
    transforms: &[TransformConfig],
) -> Result<serde_json::Value>
```

Implement these transform types (match on `TransformConfig` variants):

- **Map**: Copy/rename fields. For each `(output_field, input_field)` in the map, read `input[input_field]` and write to `output[output_field]`.
- **Drop**: Remove fields. Clone input, then `remove()` each named field.
- **AddFields**: Insert literal values. Clone input, then `insert()` each key-value pair.
- **Coalesce**: For each `(output_field, source_fields)`, take the first non-null value from the source fields.

For unimplemented variants (Regex, Template, Lookup, Filter), return an error with a clear message: `"transform type '{type}' not yet supported by the interpreter"`.

Export `pub mod interpreter` from `lib.rs`.

**Check**: `cargo test -p weavster-core` passes. Add unit tests:
- Map: `{"first_name": "Alice"}` with map `{name: first_name}` produces `{"name": "Alice"}`
- Drop: `{"a": 1, "b": 2}` with drop `["b"]` produces `{"a": 1}`
- AddFields: `{"a": 1}` with add_fields `{processed: true}` produces `{"a": 1, "processed": true}`
- Coalesce: `{"a": null, "b": "val"}` with coalesce `{result: [a, b]}` produces `{"result": "val"}`
- Combined: chain of map + drop + add_fields applied sequentially

---

### 4. Add flow file loading to Config

**Files**: `crates/weavster-core/src/config.rs`, `crates/weavster-core/src/connectors.rs`

Add a method to `Config`:

```rust
pub fn load_flows(&self) -> Result<Vec<Flow>>
```

This reads all `*.yaml` files from `{base_path}/flows/`, parses each as a `Flow`, and returns them.

Also add a method to resolve connector config references:

```rust
pub fn load_connector_config(&self, reference: &str) -> Result<ConnectorConfig>
```

The `reference` is a dotted path like `"file.input"` which maps to `connectors/file.yaml` key `input`. Parse the YAML file, navigate to the key, and deserialize as `ConnectorConfig`.

Add `walkdir` to weavster-core deps if not already there (or just use `std::fs::read_dir` since we only need one directory level).

**Check**: `cargo test -p weavster-core` passes. Add unit tests using tempdir with sample flow and connector YAML files.

---

### 5. Fix the init command to generate valid config

**Files**: `crates/weavster-cli/src/commands/init.rs`

Current problems:
- Example flow references `compute` transform (doesn't exist anywhere)
- Connector format may not match what `load_connector_config()` expects

Fix the generated files:

**`flows/example_flow.yaml`** should produce a working flow:
```yaml
name: example_flow
description: Example transformation flow
input: file.input
transforms:
  - map:
      full_name: name
      email: email
  - drop:
      - name
  - add_fields:
      processed: true
outputs:
  - file.output
```

**`connectors/file.yaml`** should be loadable by `load_connector_config()`:
```yaml
input:
  type: file
  path: "./data/input.jsonl"
  format: jsonl

output:
  type: file
  path: "./data/output.jsonl"
  format: jsonl
```

**`data/input.jsonl`** sample data:
```jsonl
{"name":"Alice Johnson","email":"alice@example.com","age":30}
{"name":"Bob Smith","email":"bob@example.com","age":25}
{"name":"Carol Williams","email":"carol@example.com","age":35}
```

**Check**: `cargo build -p weavster-cli` passes. Run `cargo run -p weavster-cli -- init /tmp/test-init` and verify the generated files parse correctly.

---

### 6. Wire the run command to use the interpreter pipeline

**Files**: `crates/weavster-runtime/src/engine.rs`, `crates/weavster-runtime/src/lib.rs`, `crates/weavster-runtime/Cargo.toml`, `crates/weavster-cli/src/commands/run.rs`, `crates/weavster-cli/src/local_db.rs`

**6a. Rewrite `Runtime` to not require a database.**

The `Runtime` struct should hold only `Config`. Remove the `PgPool` field. Remove `sqlx` import from engine.rs.

Replace `Runtime::start()` with a real implementation:

```
start(once: bool):
  1. Load flows via config.load_flows()
  2. For each flow:
     a. Resolve input connector reference → ConnectorConfig → FileInputConnector
     b. Resolve output connector references → ConnectorConfig → FileOutputConnector(s)
     c. Loop:
        - input.pull() → message (None = done)
        - interpreter::apply_transforms(&message.payload, &flow.transforms) → result
        - For each output: output.push(result)
        - Log progress
     d. Flush all outputs
     e. Log summary (messages processed, failed)
  3. If `once` is true, exit after all flows complete
```

**6b. Simplify `run.rs`** — remove all database setup logic. The command should:
1. Load config
2. Create `Runtime::new(config)`
3. Call `runtime.start(once)` (pass `--once` flag)
4. Handle Ctrl+C

**6c. Gut `local_db.rs`** — either delete it or make it a no-op module. Remove `postgresql_embedded` usage from the run path. The `LocalDatabase` struct can remain as dead code for now if removing it causes cascade changes elsewhere, but it must not be called from `run.rs`.

**6d. Update Cargo.toml files** — remove `sqlx` and `postgresql_embedded` from the required deps of weavster-runtime and weavster-cli (can stay as optional/commented). Ensure weavster-runtime depends on weavster-core.

**Check**: `cargo build` passes for all crates. `cargo test` passes. `cargo clippy -- -D warnings` clean.

---

### 7. Add --once flag to the CLI

**Files**: `crates/weavster-cli/src/main.rs`, `crates/weavster-cli/src/commands/run.rs`

Add `--once` flag to the `run` subcommand in clap. When set, the runtime processes all input and exits (no continuous polling). This is the default behavior for file connectors anyway, but the flag makes it explicit.

If `--once` is not set and all inputs are exhausted (file connectors are finite), exit gracefully with a message.

**Check**: `cargo run -p weavster-cli -- run --help` shows the `--once` flag.

---

### 8. End-to-end CLI test

**Files**: `crates/weavster-cli/tests/cli_test.rs` (new)

Using `assert_cmd` and `predicates` (already in dev-deps), write one integration test:

```rust
#[test]
fn test_init_and_run_once() {
    let dir = tempfile::tempdir().unwrap();

    // Init project
    Command::cargo_bin("weavster-cli")
        .unwrap()
        .args(["init", dir.path().to_str().unwrap()])
        .assert()
        .success();

    // Run --once
    Command::cargo_bin("weavster-cli")
        .unwrap()
        .args(["run", "--once", "--config", dir.path().to_str().unwrap()])
        .assert()
        .success();

    // Verify output
    let output = std::fs::read_to_string(dir.path().join("data/output.jsonl")).unwrap();
    let lines: Vec<serde_json::Value> = output
        .lines()
        .map(|l| serde_json::from_str(l).unwrap())
        .collect();

    assert_eq!(lines.len(), 3);
    assert_eq!(lines[0]["full_name"], "Alice Johnson");
    assert_eq!(lines[0]["processed"], true);
    assert!(lines[0].get("name").is_none()); // dropped
}
```

**Check**: `cargo test -p weavster-cli` passes including this integration test.

---

### 9. Verify everything together

Run the full verification:

```bash
cargo fmt --check
cargo clippy -- -D warnings
cargo test
cd /tmp && mkdir weavster-demo && cd weavster-demo
cargo run -p weavster-cli -- init .
cargo run -p weavster-cli -- run --once
cat data/output.jsonl
```

The output file should contain the transformed JSONL records matching the objective at the top of this plan.

**Check**: All commands succeed. Output matches expected.

---

## Completion Promise

When all 9 steps pass their checks, the following is true:
- `weavster init .` creates a valid project with sample JSONL data
- `weavster run --once` reads `data/input.jsonl`, applies map + drop + add_fields transforms, writes `data/output.jsonl`
- The output contains the correctly transformed records (renamed fields, dropped fields, added fields)
- `cargo test` passes all existing 132 tests plus new unit tests for the interpreter and connectors, plus one end-to-end CLI integration test
- No database, WASM compilation, or external services are required
- The codebase compiles, lints clean, and is formatted

---

## What this plan does NOT include (intentionally deferred)

- Database / Diesel / SQLite / PostgreSQL — not needed for file-to-file transforms
- WASM compilation pipeline — interpreter validates the IR first, WASM layers on top later
- CSV file format — JSONL is sufficient, CSV can be added as a follow-up
- Schema/column type definitions — serde_json::Value is the universal schema for now
- Structured JSON logging / per-flow tracing — existing `tracing::fmt` is adequate
- Job queue (apalis) — no continuous processing needed for MVP
- OCI packaging — no artifacts to distribute yet
- Regex/Template/Lookup transforms in interpreter — start with map/drop/add_fields/coalesce
- Connector registry abstraction — direct instantiation is fine for 1 connector type
