# Code Review — weavster

Full-codebase review of bugs, security issues, and code-quality problems.
Scope: `core/` (TS), `cli/` (TS), `engine/` (Rust), Dockerfile, Cargo manifest.
Reviewed by reading every source file. Nothing was modified.

Severity: **Critical** (exploitable / data loss) · **High** (security or correctness, likely reachable) · **Medium** (correctness/robustness, narrower) · **Low** (quality / cosmetic).

---

## Top priorities

| #   | Severity | Location                           | Issue                                                                   |
| --- | -------- | ---------------------------------- | ----------------------------------------------------------------------- |
| 1   | Critical | `cli/src/pipeline.ts:95,112`       | Path traversal — source read & sink write escape project dir            |
| 2   | High     | `cli/src/functions.ts:41`          | Arbitrary `.ts` execution via unvalidated `_ts.module` name + traversal |
| 3   | High     | `cli/src/compile.ts:163`           | `rmSync(recursive)` on user-supplied `--out` directory                  |
| 4   | High     | `engine/src/runner.rs:76`          | `names[&id]` panic defeats per-pipeline panic isolation                 |
| 5   | High     | `core/src/path.ts` / `model.ts`    | `__proto__`/`constructor` keys corrupt model objects                    |
| 6   | High     | `core/src/formats/xml.ts:32`       | XML internal-entity expansion (billion-laughs) DoS not disabled         |
| 7   | Medium   | `engine/src/connectors/file.rs:24` | Glob follows symlinks → reads outside artifact root                     |

---

## Security

### 1. Path traversal in pipeline connectors — Critical

`cli/src/pipeline.ts:95` and `:112`

```ts
const path = join(projectDir, spec.path as string); // source
const path = join(projectDir, spec.path as string); // sink
```

`spec.path` comes from pipeline YAML, schema only enforces `minLength: 1`. A pipeline with `source.path: ../../../../etc/passwd` reads arbitrary host files; a `sink.path` **writes** arbitrary files (sink does `mkdir -r`).
**Fix:** resolve and assert containment — `resolve(projectDir, p).startsWith(resolve(projectDir) + sep)`; reject otherwise. Connectors in `connectors.ts` trust the path, so containment must be enforced here.

### 2. Arbitrary code execution via `_ts.module` — High

`cli/src/functions.ts:41`

```ts
const file = resolve(projectDir, FUNCTIONS_DIR, `${name}.ts`);
...
const fn = await jiti.import(file, { default: true });
```

`name` is the flow YAML `_ts.module` value, interpolated unvalidated. `module: ../../../../tmp/evil` escapes `functions/` and jiti **executes** whatever `.ts` resolves there — in the full Node process (during `run`/`test`), not the Javy sandbox.
**Fix:** validate `name` against `/^[a-z0-9_-]+$/i` (reject `/`, `\`, `..`) before resolve, and assert the resolved path stays under `functions/`.

### 3. Destructive clean of user-supplied output dir — High

`cli/src/compile.ts:163`

```ts
rmSync(flowsDir, { recursive: true, force: true });
```

`-o/--out` is fully user-supplied and unvalidated. `weavster compile -o /important/dir` recursively deletes `/important/dir/flows`. The rm runs after `buildManifest` succeeds (good), but no check that `flows/` was weavster-created.
**Fix:** refuse to clean a `flows/` dir with foreign contents, or require `outDir` not pre-exist with unknown files.

### 4. Prototype-key corruption in canonical model — High

`core/src/path.ts:100,114` and `core/src/model.ts:78-84,99-103`

```ts
current.fields[segment] = next; // path.ts set()
current.fields[last] = value;
fields[key] = fromValue(child); // model.ts fromValue()
```

`fields` is a plain `{}`. A segment/key of `__proto__` assigns through the prototype setter, corrupting the object (and `constructor`/`prototype` set own-looking keys with surprising effects). **Note:** global `Object.prototype` pollution is largely blocked by the `kind !== 'object'` descend guards, so this is robustness/correctness rather than full RCE — but still reachable from untrusted JSON/XML input and DSL path strings.
**Fix:** back `fields` with `Object.create(null)`, or deny `__proto__`/`constructor`/`prototype` keys/segments at the model intake and in `parsePath`/`set`.

### 5. XML internal-entity expansion (billion laughs) — High

`core/src/formats/xml.ts:32-40`

```ts
const parser = new XMLParser({ ... });   // no processEntities / DTD policy
```

`fast-xml-parser` does not fetch external entities (no classic XXE file disclosure), but processes **internal DTD entities** unless `processEntities: false`. Nested entity definitions can blow up memory (DoS). No input-size cap anywhere. `wellFormedValidator` (`:71-88`) also parses the whole text **twice** (validate + parse), doubling cost.
**Fix:** set `processEntities: false`, reject `<!DOCTYPE`/`<!ENTITY` in untrusted input, add a size cap.

### 6. Symlink escape in file connector glob — Medium (security)

`engine/src/connectors/file.rs:24-31`
`manifest::check_contained` rejects `..`/absolute in the manifest _string_, but `glob::glob` follows symlinks. A symlink inside the artifact (`in/link -> /etc`) reads host files outside the root. Artifact is the compiled user project (attacker-influenced).
**Fix:** canonicalize each matched path and verify it stays under the canonicalized root before reading.

### 7. Unbounded input file read — Medium (security/DoS)

`engine/src/connectors/file.rs:47-49`

```rust
tokio::fs::read_to_string(path).await   // whole file → String, no cap
```

Each matched file is read fully into memory then cloned into the envelope. A multi-GB file is an unbounded host allocation driven by artifact-controlled content. Non-UTF-8 files also fail with a confusing I/O error.
**Fix:** cap input size (or stream); map non-UTF-8 to a clear error.

### 8. Unbounded concurrent wasm stores — Medium (security/DoS)

`engine/src/host.rs:23,26,126`
Each in-flight doc gets a fresh `Store` capped at 256 MB + 64 MB stdout, in its own `spawn_blocking`. No global concurrency cap → N pipelines drive aggregate guest memory to N×(256+64) MB, OOM-killing the distroless container (no swap).
**Fix:** bound concurrent transforms with a semaphore / bounded `JoinSet`.

### 9. Dependency / image hygiene — Medium

- `engine/Cargo.toml:21-22` — `wasmtime`/`wasmtime-wasi` pinned `34.0.2` (not latest). This is the entire sandbox trust boundary. **Fix:** add `cargo audit` to CI; track RUSTSEC advisories for 34.x.
- `engine/Dockerfile:15,23` — base images on mutable tags (`rust:1.90-slim-bookworm`, `gcr.io/distroless/cc-debian12`). **Fix:** pin by `@sha256:` digest. (Good: runs as `nonroot` 65532, distroless = no shell.)

---

## Bugs

### 10. `names[&id]` panics, defeating panic isolation — High

`engine/src/runner.rs:76`

```rust
Ok((id, Err(err))) => failures.push((names[&id].clone(), ...)),   // panic-on-missing
```

Line 78 correctly uses `.get(...).unwrap_or_default()`. Line 76's index panic crashes the whole run — the exact failure the id→name map exists to prevent.
**Fix:** `names.get(&id).cloned().unwrap_or_default()` to match line 78.

### 11. `unawaited program.parseAsync()` — Medium

`cli/src/index.ts:22`

```ts
program.parseAsync();
```

Async command rejections are unhandled; `process.exitCode` set in actions can be lost.
**Fix:** `program.parseAsync().catch((e) => { console.error(e); process.exit(1); });`

### 12. `deepEqual` via `JSON.stringify` is key-order-sensitive — Medium

`cli/src/fixtures.ts:114-116` and `core/src/dsl/expr.ts:32`

```ts
return JSON.stringify(a) === JSON.stringify(b);
```

`{a,b}` ≠ `{b,a}`; drops `undefined`; throws on cycles. Causes spurious fixture failures and wrong `_eq`/`_in`/`_select` results when a transform reorders keys.
**Fix:** structural, key-order-insensitive deep-equal.

### 13. `fileSource` TOCTOU — Medium

`cli/src/connectors.ts:15-26`

```ts
await access(path); ... yield await readFile(path, 'utf8');
```

File can change between check and read.
**Fix:** drop `access`, just `readFile`, map `ENOENT` to the friendly message.

### 14. Unchecked numeric casts in `_gt`/`_lt` — Medium

`core/src/dsl/expr.ts:80-87`

```ts
return (evalExpr(a, ctx) as number) > (evalExpr(b, ctx) as number);
```

Non-number operands coerce silently (`"10" < "9"` is `true`; `undefined > x` is `false`).
**Fix:** validate both sides numeric, throw `TransformError` otherwise.

### 15. Unbounded recursion in DSL & model — Medium (DoS)

`core/src/dsl/engine.ts` (`runSteps`/`_when` nesting, `:116-126,160-176`) and `core/src/model.ts:74-89` (`fromValue`). No depth/step-count limit; a crafted flow or deeply nested JSON can stack-overflow.
**Fix:** add a depth/op-count guard at both entry points.

### 16. Silent path mis-parse — Medium

`core/src/path.ts:13,19` — `TOKEN` regex with `matchAll` skips invalid input instead of erroring: `a..b` → `['a','b']`, `a[01]`/`a[1abc]` mis-parse. Linear (no ReDoS) but lossy.
**Fix:** validate the whole string is consumed; reject malformed paths.

### 17. Inherited-key existence checks — Low/Medium

`core/src/path.ts:54,97,132` — `segment in current.fields` / `current.fields[segment] === undefined` match inherited members (`toString`, etc.), mishandling those key names.
**Fix:** `Object.hasOwn(current.fields, segment)`.

### 18. Epoch deadline integer-division fragility — Low/Medium

`engine/src/host.rs:128`

```rust
let deadline_ticks = WALL_CLOCK_LIMIT.as_millis() / EPOCH_TICK.as_millis();
```

Currently `100`, but if `WALL_CLOCK_LIMIT < EPOCH_TICK` it rounds to `0` → `set_epoch_deadline(0)` traps every document.
**Fix:** `.max(1)` or assert the invariant.

### 19. Misreported output-cap / empty-output traps — Medium

`engine/src/host.rs:138-147` — a guest writing >64 MB traps at the WASI boundary, reported as generic "memory/time limit or internal error"; clean-exit-no-write and partial-write-then-trap are conflated. Host stays up, but diagnostics mislead.
**Fix:** distinguish output-size-cap and no-output cases.

### 20. `run` command ignores path argument — Medium (UX)

`cli/src/commands/run.ts:9` hardcodes `runPipelines('.', name)` — no `[path]` arg, unlike `compile`/`test`/`validate`. Only works from project cwd.
**Fix:** add `[path]` arg for consistency.

### 21. `.js` bundle can leak into artifact on crash — Low

`cli/src/compile.ts:143-150` — `writeFileSync(jsPath)` then javy, removed in `finally`. SIGKILL mid-compile leaves a stray `<flow>.js` in `flows/`, violating the "only .wasm" invariant.
**Fix:** write the temp `.js` to `os.tmpdir()`, not inside `flowsDir`.

### 22. Misattributed fixture error label — Low

`cli/src/fixtures.ts:99-102` — failures inside `applyFlow` (transform) are labeled `INPUT_FILE`.
**Fix:** separate parse and transform try blocks.

---

## Code quality

- **`bundle.ts:18-22`** — `HAZARDS` substring regexes (`/\basync\b/` etc.) false-positive on the words inside string/comment data, blocking valid bundles. Document the heuristic, or strip strings/comments first; rely on esbuild `platform:neutral` for the real guarantee. (Low)
- **`runner.rs:4-8,102`** — "FIFO queue, concurrency 1" comment overstates a plain serial `while` loop; no queue/pipelining. Doc-only. (Low)
- **`cli/src/runner.ts:78-81`** — re-declares `Source`/`Sink` structural types instead of importing them from `connectors.ts`. (Low)
- **`engine/Cargo.toml:19,25`** — `serde_json` listed in both `[dependencies]` and `[dev-dependencies]`; the dev entry is redundant. (Low)
- **`core/src/dsl/engine.ts:50-55` vs `:63`** — `_set` pre-evaluates all expressions before writing; `_default` evaluates lazily in-loop. Inconsistent and undocumented; later paths in one `_set` can't see earlier writes. Confirm intended. (Low/Medium)
- **`core/src/dsl/engine.ts:108`** — `_select` aliases `working.meta` by reference, re-sharing the `errors` array. (Low)
- **`cli/src/commands/*`** — inconsistent stdout/stderr usage for success messages (`run`/`compile` → stderr, `init`/`test` → stdout). (Low/cosmetic)
- **`engine/src/log.rs`** — `emit` is a one-line wrapper over `eprintln!`; thin abstraction (documented future-tracing seam). (Low)
- **Tests** — `engine/tests/*` temp dirs keyed on `process::id()` only; cleanup via `.ok()` leaks dirs on panic. Test-only. (Low)

---

## Notes (checked, not issues)

- `cli/src/javy.ts:24` — `spawnSync(execPath, [...args])` array form, **no shell** → no command injection. Correct pattern.
- YAML parsing (`yaml` lib) uses safe defaults — no code-exec deserialization; alias expansion capped by the library.
- `engine/Dockerfile` — runs as `nonroot` (65532), distroless base (no shell/pkg manager). Strong baseline.
- `engine/src/registry.rs`, `connectors/mod.rs` — clean, no issues.
