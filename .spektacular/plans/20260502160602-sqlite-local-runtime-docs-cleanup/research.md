# Research: 20260502160602-sqlite-local-runtime-docs-cleanup

## Alternatives considered and rejected

### Option A: Merge stale PR #36 as-is

PR #36 broadly replaces embedded PostgreSQL references and removes the local port field.

**Rejected**: Current main has superseded most of PR #36's docs shape, and `git merge-tree` shows conflicts in README and docs pages. The PR would also reintroduce old docs concepts such as `database.url` and old first-flow wording, which conflicts with the current status baseline.

### Option B: Remove `runtime.local.port` from the config API

This would delete the field from `LocalConfig`, remove the default helper, and update tests.

**Rejected**: Existing tests at `crates/weavster-core/src/config.rs:1155` and `crates/weavster-core/tests/config_integration_test.rs:390` prove the field currently parses. Removing it would turn a wording cleanup into a compatibility change.

### Option C: Leave source comments unchanged

This would close PR #36 and rely on README/docs wording from the prior docs baseline.

**Rejected**: A focused scan still finds embedded PostgreSQL comments in `crates/weavster-core/src/config.rs:135`, `crates/weavster-core/src/config.rs:145`, and `crates/weavster-core/src/config.rs:149`, which keeps the stale framing alive for contributors.

## Chosen approach — evidence

The chosen approach is to update generated starter config, docs examples, and source-facing comments while preserving config compatibility.

- `crates/weavster-cli/src/commands/init.rs:54` shows the generated starter config still includes `port: 5433`, even though the local state path is SQLite.
- `docs/docs/configuration/project.md:19` shows the recommended project config example still includes `port: 5433`.
- `crates/weavster-core/src/config.rs:135`, `crates/weavster-core/src/config.rs:145`, and `crates/weavster-core/src/config.rs:149` contain the remaining embedded PostgreSQL source comments.
- `crates/weavster-core/src/config.rs:1155` and `crates/weavster-core/tests/config_integration_test.rs:390` confirm the compatibility boundary: local port values still parse.

## Files examined

- `crates/weavster-cli/src/commands/init.rs:54` — generated starter config includes the unused local port.
- `crates/weavster-core/src/config.rs:135` — local mode comment still names embedded PostgreSQL.
- `crates/weavster-core/src/config.rs:145` — data directory comment still names embedded Postgres.
- `crates/weavster-core/src/config.rs:149` — local port comment still names embedded PostgreSQL.
- `crates/weavster-core/src/config.rs:1155` — unit test asserts a custom local port parses.
- `crates/weavster-core/tests/config_integration_test.rs:390` — integration test asserts profile resolution preserves local port values.
- `docs/docs/configuration/project.md:19` — project config example includes the local port.
- `docs/docs/configuration/project.md:49` — project field table marks the local port as partial.
- `README.md:169` — README already clearly states local state uses SQLite, not embedded PostgreSQL.
- `docs/docs/getting-started/first-flow.md:89` — first-flow docs already clearly state local state uses SQLite, not embedded PostgreSQL.

## External references

None. The current codebase and PR history are sufficient for this cleanup.

## Prior plans / specs consulted

- `.spektacular/plans/20260428024252-docs-current-vs-planned-alignment/plan.md` — established current/partial/config-only status labels and documented SQLite local state.
- `.spektacular/specs/20260502160602-sqlite-local-runtime-docs-cleanup.md` — defines the cleanup boundary and non-goals.

## Open assumptions

No open assumptions. The compatibility boundary is verified by existing tests and the runtime behavior is not changed.

## Rehydration cues

Re-run:

```bash
rg -n "embedded PostgreSQL|Embedded PostgreSQL|embedded Postgres|runtime\\.local\\.port|port: 5433|Port for embedded|default_pg_port" . --glob '!target' --glob '!docs/build'
```

Then re-read `crates/weavster-core/src/config.rs`, `crates/weavster-cli/src/commands/init.rs`, and `docs/docs/configuration/project.md`.
