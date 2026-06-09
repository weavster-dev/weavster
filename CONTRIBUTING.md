# Contributing to Weavster

Weavster is built in small, reviewable slices. The goal is a codebase that stays
understandable as it grows.

## Workflow

- Keep one active milestone at a time.
- Create a short branch (or stacked PR) per small slice. One concept per PR.
- Prefer a failing-test commit before the implementation commit.
- Keep refactors separate from behavior changes.
- Keep docs next to the feature they explain.

## Where the code lives

Most of Weavster is a pnpm/TypeScript monorepo (`core/`, `cli/`, `website/`). The production
runtime is the **Rust engine** in [`engine/`](engine/) — a Cargo workspace at the repo root
([RFC 0003](docs/rfcs/0003-engine-runtime.md)). The two are built with separate toolchains and
must not mix: the TS side builds the CLI that _compiles_ WASM artifacts; the engine only _runs_
them, so no Node or TS toolchain enters the engine build or its image. Engine work uses
`cargo build|clippy|test --workspace`; everything else uses `pnpm`.

## Commit conventions

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(validate): validate config against schema
docs(spec): define config v0alpha1
test(dsl): add operation fixtures
```

One commit should do one thing.

## Pull request rules

- One PR per task slice, not one PR per milestone.
- Target roughly one concept per PR.
- Include passing tests, or clearly mark intentional failing-test-first groundwork.
- Fill out the PR template, including the docs and test checklist.

## Docs update policy

Documentation is part of the feature, not a follow-up.

- Every user-visible change updates docs/examples, or explicitly records "no docs impact".
- Every new CLI command or option updates the CLI reference.
- Every format pack ships with its own docs and example fixtures.
- The docs build must pass in CI before merge.

## Definition of done

A task is done only when:

- Behavior is documented.
- Tests exist and pass locally.
- The diff was reviewed file by file.
- The commit message is clear.
- No speculative abstraction was introduced (no new abstraction until two real uses).
- `notes/DEV_LOG.md` has an entry for the slice.
