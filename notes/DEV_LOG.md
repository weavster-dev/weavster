# Dev Log

Newest entries on top. One entry per merged slice.

## Template

```
## YYYY-MM-DD — <slice name>
- What changed:
- What I learned:
- What is next:
```

---

## 2026-06-05 — v0alpha2 slice 1: expression evaluator + \_set/\_unset

- What changed: Started the v0alpha2 DSL (RFC 0001) as new modules under `core/src/dsl/` so it
  can land in stacked slices while v0alpha1 keeps powering the CLI; the cutover (CLI adopts v2,
  v1 removed) is the final slice. `dsl/expr.ts` is the evaluator: a value position is an
  expression — `$path` reads from the working doc, `$$x` is the literal `$x`, a single
  `_`-prefixed key is an operator, and plain arrays/objects evaluate deeply so refs/operators
  nest anywhere. `_lit` returns its arg verbatim (the escape). `dsl/engine.ts` runs a pipeline
  of single-key `_op` steps, patch-by-default; slice 1 ships `_set` (set named paths, keep the
  rest) and `_unset` (remove paths). `dsl/errors.ts` holds `TransformError`.
- What I learned: Two deliberate semantics. (1) A missing reference evaluates to `undefined` and
  `_set` _skips_ that key — so copying a maybe-absent field is a no-op, not a null write; an
  explicit `null` literal still writes. (2) `_set` evaluates all its values against the
  step-start document before applying any, so sibling keys are independent (`{ a: 2, b: $a }`
  reads the original `a`) — predictable, matches Mongo `$set`. The `$`/`_`/plain trichotomy
  lives entirely in the evaluator, so structural ops just call `evalExpr` and never parse sigils
  themselves. Confirmed decisions: fuller operator set, version implied by project, replace-in-
  place via a final cutover slice, stacked delivery.
- What is next: v0alpha2 slice 2 — remaining structural ops (`_rename`/`_append`/`_select`/
  `_when`) and the value operators (`_concat`, string/date, `_eq`/`_gt`/`_in`/`_and`/…, `_cond`).

## 2026-06-05 — M8 TypeScript escape hatch

- What changed: Added the `ts` op — a custom-code escape hatch. Where custom code enters and
  leaves the system: the CLI's `cli/src/functions.ts` scans a flow (recursing into `when`
  branches) for `ts` steps, loads each `functions/<module>.ts` via jiti (runtime TS import, no
  build step), and passes a `{ name: fn }` registry to `applyFlow(doc, flow, { functions })`.
  In core, the `ts` op reads `from` (default the whole document) as a native JS value, calls the
  function, takes the result through a JSON boundary (`JSON.parse(JSON.stringify(...))`), and
  writes it to `to` (default the root). Engine stays pure — it never touches the filesystem or
  jiti; functions are injected. Added the `ts` schema variant, a golden-path example function
  (`initials.ts`) wired into the order flow, a harness project, the TypeScript Transforms docs,
  and tests.
- What I learned: The JSON boundary is doing real work — it both enforces the portability
  contract (anything non-JSON, e.g. a function value, is dropped) and mirrors exactly what will
  cross the WASM boundary in production, so a function authored here ports unchanged. Keeping
  function loading in the CLI (not core) preserved core's purity and unit-testability — core
  tests inject fake functions; only the CLI depends on jiti + the filesystem. A real bug:
  `jiti.import` resolves a relative path against jiti's own base, not the cwd, so the module path
  must be absolutized (`resolve(...)`) before import — the `existsSync` check passed on a
  cwd-relative path while jiti failed on the same string. `runFixtures` went async because jiti
  loads are async; the test command already used `parseAsync`.
- What is next: v0alpha2 DSL (RFC 0001) — resolve open questions, then implement; M9 after.

## 2026-06-05 — RFC 0001: v0alpha2 DSL (design)

- What changed: Wrote `docs/rfcs/0001-v0alpha2-dsl.md` capturing the next-gen transform DSL
  decided in discussion. Core idea: a MongoDB-flavored expression language on a mutate-in-place
  pipeline. Two sigils — `$path` reads a reference, `_op` invokes an operator (as a step or a
  value). Patch by default (`_set`/`_default`/`_unset`/`_rename`/`_append` keep the rest of the
  document); reshape (`_select`) is the explicit opt-in. The M7 cleanup folds in: maps remove the
  `at`/`to` split, `map` becomes `_set: { to: $from }`, str/date/concat become value operators,
  and single-key `_op` dispatch fixes the noisy validation.
- What I learned: The decisive constraint was partial edits — the HL7 case ("set MSH-4, reformat
  one date, append one PID, leave the rest") rules out pure projection. Mongo's split (`$set`
  patch vs `$project` reshape) is the resolution, so v0alpha2 is patch-first with reshape opt-in,
  not projection-first. Expressions-as-values are what unlock composition and future macros (the
  reuse goal); the structural op surface actually shrinks because transforms move into the
  expression namespace. Design only — implementation waits until after M8 so the escape hatch
  lands on the stable v0alpha1 DSL first.
- What is next: M8 — TypeScript escape hatch (still on v0alpha1); resolve RFC open questions, then
  implement v0alpha2.

## 2026-06-05 — M7 slice 4: wire flows into the cli

- What changed: Connected the engine to the CLI (the cli↔core integration deferred since M3).
  `@weavster/cli` now depends on `@weavster/core`. New `cli/src/flow.ts` loads + schema-validates
  a flow by name (`flows/<name>.yaml`) and lists/validates all flows. `weavster test` was rewritten:
  fixtures are now grouped by flow under `fixtures/<flow>/<case>/`; each case's `input.json` is
  `json.parse`d, run through `applyFlow(doc, flow)`, and `toValue`d for comparison against
  `expected.json`. `weavster validate` also validates every `flows/*.yaml`. The golden-path example
  gained a real `flows/order.yaml` (str/concat/when) with transformed fixtures; harness tool-test
  fixtures were restructured (pass/fail/badflow). Added a vitest alias so cli tests resolve
  `@weavster/core` from source without a build; `cli:build` now builds core first.
- What I learned: vitest resolves `@weavster/core` to `core/src` via an alias, but runtime
  (tsx dev, built dist) resolves the package's `dist` entry — so `cli:build` must build core
  before cli, and CI already does. The fixture layout encodes the flow mapping in the path
  (`fixtures/<flow>/<case>`), so no per-case pointer file is needed. Rough edge: a bad `op`
  produces a noisy ajv error list because the step schema is a `oneOf` over op variants — every
  branch reports why it failed. Acceptable for now; a future improvement is to discriminate on
  `op` first and validate against just that variant. The golden path now proves the whole
  thesis end to end: parse → canonical model → declarative flow → emit → compare.
- What is next: M8 — TypeScript escape hatch.

## 2026-06-05 — M7 slice 3: conditional logic

- What changed: Added the `when` op. `cond` is a single predicate — a `path` tested with
  `equals` (literal match) or `exists` (boolean presence). `then` runs when it holds, `else`
  (optional) otherwise. To support nested branches, the engine's step loop was extracted into
  `runSteps(working, steps)`, which `applyFlow` and `when` both call; branches are full
  sub-pipelines. Extended `flow.schema.json` (recursive `then`/`else` via `$ref` to the step
  def), the valid sample, the DSL docs, and added `when` tests.
- What I learned: Recursion fell out cleanly once the per-step try/catch lived in `runSteps`
  rather than `applyFlow` — a failure inside a branch throws a `TransformError` tagged with its
  own index, and the enclosing `when` step re-wraps it, so the message nests:
  `step 0 (when): step 0 (map): no value at "missing"`. Predicate semantics chosen for
  least surprise: `equals` against a missing path or a non-scalar is simply false (not an
  error), so authors branch on shape without guarding first; only a malformed `cond` (no
  `equals`/`exists`) throws. Kept the predicate to one comparison — AND is nesting, NOT is the
  `else` branch — to avoid growing a boolean expression language this slice.
- What is next: M7 slice 4 — wire flows into the cli (load `flows/`, run via the fixture
  harness) and finish the golden-path + DSL reference.

## 2026-06-04 — M7 slice 2: concat + string/date helpers

- What changed: Added three ops to the engine. `concat` joins a `parts` list — each part a
  `path` to read or a literal `value` — into a string at `to`, with an optional `sep`. `str`
  applies `upper`/`lower`/`trim` from `from` to `to` (default `from`, so in-place). `date`
  applies `toIso`, parsing the source with `new Date(...)` and writing `.toISOString()`.
  Extended `flow.schema.json` with the three variants + a richer valid sample, the DSL docs,
  and added a combined-pipeline test using the helpers.
- What I learned: A real JSON Schema trap — `additionalProperties: false` only sees
  `properties` declared in the _same_ schema object, not ones nested inside `oneOf`. The first
  cut put each concat part's `path`/`value` only inside `oneOf` branches, so `false` treated
  both as unknown and rejected every part. Fix: declare `path` and `value` at the item level,
  use `oneOf` only to require exactly one. Determinism mattered for `date`: `toIso` is pure
  given its input (no `now`), so it is safe for fixtures; `new Date('2026-06-04')` yields a
  fixed UTC instant. Scalar coercion lives in one helper (`scalarToString`) so concat/str/date
  share the same "null → empty, structured → error" rule.
- What is next: M7 slice 3 — conditional logic.

## 2026-06-04 — M7 slice 1: transform engine + map/rename/default

- What changed: Added the transform engine at `core/src/transform.ts`. `applyFlow(doc, flow)`
  clones the input document, then runs an op-keyed `steps` list as a mutate-in-place pipeline,
  returning a new document (input is never mutated). First ops: `map` (copy `from`→`to`),
  `rename` (move), `default` (fill `at` only if absent; literal `value` via `fromValue`).
  Added `set`/`remove`/`PathError` to `core/src/path.ts` (set auto-creates missing object
  segments, refuses to grow arrays). Defined the `flow.schema.json` contract + valid/invalid
  sample flows, and started the Transform DSL docs page. This slice is core-only; wiring the
  cli fixture harness to load `flows/` and run the engine comes in slice 4.
- What I learned: Execution-path trace — `applyFlow` deep-clones `doc.root` into a working
  document, then for each step looks up the op in a registry and calls it; the op reads via
  `get` and writes via `set`/`remove` against the working tree; any throw is re-wrapped as a
  `TransformError` tagged with the step index + op, and the pipeline stops. Keeping the engine
  in core with no ajv dependency means flow _loading + schema validation_ will live in the cli
  (slice 4) where ajv already is — the engine only ever sees an already-parsed `Flow`. The
  mutate-in-place model makes `rename` literally copy-then-`remove`, and `default` a guarded
  `set`, which is why the path helpers (not the ops) own the structural rules.
- What is next: M7 slice 2 — `concat` + string/date helpers.

## 2026-06-04 — M6 XML format pack

- What changed: Added the XML format pack at `core/src/formats/xml.ts` (`xml` namespace),
  built on fast-xml-parser. `xml.parse(text, validator?)` runs a validator, then maps via
  `fromValue` to a `Document` tagged `sourceFormat: 'xml'`. Convention: attributes → `@`-prefixed
  fields, element text → `#text`, repeated elements → arrays, single elements → objects, and
  leaf values stay strings (no coercion). `xml.serialize` renders indented XML with a trailing
  newline. Added the `XmlValidator` interface with a default `wellFormedValidator` (seam for XSD
  later), 12 tests, and extended the Format Packs docs with a JSON/XML comparison + limitations.
- What I learned: XML→object mapping is where format quirks try to leak in, so the pack
  flattens them into ordinary fields the model already understands — a transform addressing
  `line[0]` cannot tell JSON from XML. Where XML genuinely differs from JSON: leaves are
  untyped (everything is a string), and a single element is ambiguous between object and
  one-element array (documented limitation). A concrete round-trip trap: the XMLBuilder
  default `suppressBooleanAttributes: true` renders `vip="true"` as a valueless `vip`, which
  is not well-formed XML and fails on reparse — set it to `false`. Validation is a pluggable
  interface so XSD support can drop in without touching parse/serialize; malformed input
  throws `XmlParseError`, mirroring the JSON pack.
- What is next: M7 — declarative transform DSL.

## 2026-06-04 — M5 JSON format pack

- What changed: Added the JSON format pack at `core/src/formats/json.ts`, exported as the
  `json` namespace from `@weavster/core`. `json.parse(text)` runs `JSON.parse` then
  `fromValue` to produce a `Document` tagged `sourceFormat: 'json'`, throwing `JsonParseError`
  on invalid input. `json.serialize(docOrNode)` runs `toValue` then `JSON.stringify` with a
  2-space indent and trailing newline. Added round-trip + stability tests, a richer nested
  JSON case to the golden-path example, and a Format Packs docs page (wired into the sidebar).
- What I learned: The format pack is deliberately thin — it owns only text⇄value, while the
  model owns value⇄node (`fromValue`/`toValue`). That split is what lets one transform serve
  many formats: by the time a transform runs, format is gone. Decisions: the pack lives as a
  module inside `@weavster/core` (not its own package) to avoid cross-package resolution and
  build-before-test friction — it can be extracted later if a real need appears. Syntax errors
  throw (`JsonParseError`); `meta.errors` stays reserved for semantic validation in a later
  milestone. The cli fixture harness is intentionally NOT rewired to the pack yet — that
  integration belongs with the engine (M7+), keeping cli↔core decoupled for now.
- What is next: M6 — XML format pack (parser, serializer, map into the canonical model).

## 2026-06-03 — M4 canonical document model

- What changed: Created the `@weavster/core` package holding the canonical model.
  `core/src/model.ts` defines `Node` as a tagged union of `scalar`/`object`/`array`, a
  `Document` wrapping a root node with `{ sourceFormat, errors }` metadata, type guards,
  and `fromValue`/`toValue` to normalize native JS values to/from nodes.
  `core/src/path.ts` defines path addressing: segment arrays are canonical (strings =
  object fields, numbers = array indices), with `parsePath`/`formatPath` for the dotted +
  bracket string form (`lines[0].sku`) and `get`/`getValue` to resolve a path to a node or
  value. Added the package to the pnpm workspace, switched root `test` to `pnpm -r test`,
  added a core build step to CI, and wrote the Concepts page.
- What I learned: The model is the seam that lets one transform serve many formats — by
  the time a transform runs, format is gone and only nodes remain (M5 JSON / M6 XML both
  target the same three kinds). Decisions: a tagged union (not native values + sidecar
  metadata) makes XML attributes/text/order representable later without reshaping; the
  dotted+bracket path syntax keeps a numeric object key (`counts.0`, string) distinct from
  an array index (`counts[0]`, number). `fromValue`/`toValue` are the model's intake
  boundary; format packs own only text⇄value, the model owns value⇄node. vitest runs TS
  without typechecking, so CI builds core with `tsc` to catch type errors.
- What is next: M5 — JSON format pack (parse/serialize, map into the canonical model).

## 2026-06-03 — M3 fixture test harness

- What changed: Added `weavster test [path]`. `cli/src/fixtures.ts` scans a project's
  `fixtures/` directory (one folder per case, each with `input.json` + `expected.json`),
  runs each input through `runFlow`, deep-compares the result to expected, and builds a
  line-by-line JSON diff on mismatch. `cli/src/commands/test.ts` prints `✓`/`✗` per case,
  a passed count, and sets exit code 1 on any failure. Created `examples/golden-path/`, a
  real user project (matching the `weavster init` layout) used as a CI smoke test, plus
  tool-test fixtures under `tests/fixtures/harness/` (passing + failing). Wrote the testing
  guide and `test` CLI docs.
- What I learned: M3 has no transform engine, so `runFlow` is an identity passthrough —
  output equals input, and a fixture passes when `expected.json` matches `input.json`. The
  harness is deliberately decoupled from the flow: M4–M6 swap the body of `runFlow` for the
  canonical model + transform DSL without touching loader, compare, or diff. The data flow
  is path → `fixtures/` scan → per-case parse → `runFlow` → `deepEqual` → diff. Keeping
  "tool-test fixtures" (`tests/fixtures/`, verify the tool) separate from "user-project
  fixtures" (a project's `fixtures/`, verified by `weavster test`) avoids confusion.
- What is next: M4 — canonical document model.

## 2026-06-02 — M2 config schema and validation

- What changed: Defined the `v0alpha1` project schema (`spec/schemas/project.schema.json`):
  required `apiVersion` (const `weavster/v0alpha1`) and `name` (kebab pattern), optional
  `description`, and `additionalProperties: false`. Added the `@weavster/cli` package with
  `weavster validate [path]` — resolves `weavster.yaml`, parses YAML, validates with Ajv,
  and prints path-aware errors. Added valid + 4 invalid sample configs, a vitest suite, and
  a `ci` workflow.
- What I learned: A schema-failing config: `name: Orders To Warehouse` fails the
  `^[a-z0-9][a-z0-9-]*$` pattern (spaces and uppercase not allowed). Schema validation here is
  shape/type checking only — it cannot catch deeper problems like a flow referencing a field
  that does not exist; that is compile-time validation, which comes in later milestones.
  Ajv v8 needs the named import `{ Ajv }` to construct cleanly under TypeScript NodeNext.
- What is next: M3 — fixture test harness (`weavster test`).

## 2026-06-02 — M1 documentation platform

- What changed: Scaffolded a Docusaurus TypeScript site in `website/`, wired the repo
  as a pnpm workspace (root `package.json` + `pnpm-workspace.yaml`), set Weavster config
  (title, GitHub Pages URL/baseUrl, nav, footer, blog disabled), replaced the sample
  tutorial content with an explicit sidebar and 7 placeholder pages, and added two CI
  workflows: `docs-build` (PRs) and `docs-deploy` (GitHub Pages on merge to main).
- What I learned: Docs are built with `pnpm docs:build` (delegates to `docusaurus build`
  in `website/`). Nav/footer live in `website/docusaurus.config.ts`, page order in
  `website/sidebars.ts`, pages in `website/docs/*.md`. Deploy publishes `website/build`
  via `upload-pages-artifact` + `deploy-pages`; GitHub Pages must be set to "GitHub Actions"
  as its source in repo settings for the deploy job to succeed.
- What is next: M2 — config schema and validation (`weavster validate`).

## 2026-06-02 — M0 reboot foundation

- What changed: Added `.gitignore`, `.editorconfig`, Prettier config, `CONTRIBUTING.md`,
  PR template, and `notes/DEV_LOG.md`. Created the top-level folder structure from
  `MVP_PLAN.md` and moved the planning docs into `docs/`.
- What I learned: Repo was effectively greenfield (only `CLAUDE.md` + `LICENSE` tracked),
  so no legacy code to freeze. New direction is a Node/TS stack per the plan.
- What is next: M1 — scaffold the Docusaurus site in `website/` and wire up docs CI.
