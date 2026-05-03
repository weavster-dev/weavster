# Context: 20260502160602-sqlite-local-runtime-docs-cleanup

## Current State Analysis

Current main already documents SQLite as the local state store in the README and docs, but a focused scan still finds stale or misleading local-runtime remnants:

- `crates/weavster-core/src/config.rs:135` — `RuntimeMode::Local` comment still says embedded PostgreSQL.
- `crates/weavster-core/src/config.rs:145` — `LocalConfig.data_dir` comment still says embedded Postgres.
- `crates/weavster-core/src/config.rs:149` — `LocalConfig.port` comment still says port for embedded PostgreSQL.
- `crates/weavster-core/src/config.rs:167` — `default_pg_port` helper name and comment preserve the old Postgres framing.
- `crates/weavster-cli/src/commands/init.rs:54` — generated starter config still writes `runtime.local.port: 5433`.
- `docs/docs/configuration/project.md:19` — docs example still includes the local port value.
- `docs/docs/configuration/project.md:49` — docs table currently labels the field as partial and ties it to SQLite-vs-Postgres wording.
- `crates/weavster-core/src/config.rs:1155` and `crates/weavster-core/tests/config_integration_test.rs:390` — existing tests assert local port values still parse, which is the compatibility behavior this plan preserves.

## Per-Phase Technical Notes

### Phase 1.1: Clean up SQLite local runtime wording

- `crates/weavster-cli/src/commands/init.rs:50` — remove the generated `port: 5433` entry from the default `weavster.yaml` template.
- `crates/weavster-core/src/config.rs:135` — change local mode comment from embedded PostgreSQL to SQLite-backed local state.
- `crates/weavster-core/src/config.rs:145` — change local data-dir comment from embedded Postgres to SQLite state and caches.
- `crates/weavster-core/src/config.rs:149` — change local port comment to compatibility-only wording.
- `crates/weavster-core/src/config.rs:150` and `crates/weavster-core/src/config.rs:167` — rename `default_pg_port` to a neutral local-port default helper, leaving the default value unchanged.
- `docs/docs/configuration/project.md:14` — remove `port: 5433` from the recommended project config example.
- `docs/docs/configuration/project.md:49` — adjust the local port row to describe the field as legacy/compatibility-only rather than part of the local SQLite runtime.
- `crates/weavster-core/src/config.rs:1140` and `crates/weavster-core/tests/config_integration_test.rs:350` — leave existing tests with local port values intact to prove compatibility.

**Complexity**: Low
**Token estimate**: ~8k
**Agent strategy**: Single agent, sequential execution

## Testing Strategy

Run the existing Rust verification suite because the cleanup touches generated config and config comments/helper naming. Existing config tests should continue to cover legacy local port parsing. Add no new tests unless editing the init template exposes an existing CLI test expectation that needs a direct assertion update.

Also run a targeted stale-claim scan across README, docs, and Rust source for embedded PostgreSQL local-runtime claims and local port recommendations.

## Project References

- `.spektacular/specs/20260502160602-sqlite-local-runtime-docs-cleanup.md` — approved scope for this replacement cleanup.
- `.spektacular/plans/20260428024252-docs-current-vs-planned-alignment/plan.md` — prior docs baseline and status-label model.
- `https://github.com/weavster-dev/weavster/pull/36` — stale PR being replaced with this narrower current-main cleanup.

## Token Management Strategy

| Tier | Token Budget | Agent Strategy |
|------|-------------|----------------|
| Low | ~10k | Single agent, sequential |
| Medium | ~25k | 2-3 parallel agents |
| High | ~50k+ | Parallel analysis, sequential integration |

This plan is low-tier because it changes a small set of comments, templates, docs examples, and compatibility-preserving helper names.

## Migration Notes

No migration is required. Existing projects may keep `runtime.local.port`; it remains accepted but is not recommended in new starter projects.

## Performance Considerations

No runtime performance impact is expected. The cleanup does not change state-store selection or execution behavior.
