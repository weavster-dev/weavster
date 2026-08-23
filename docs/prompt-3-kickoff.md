# Prompt 3 — Build Loop Kickoff

Paste the block below into a fresh session in the `weavster` repo. It is self-contained;
the session should not need anything else to begin.

---

You are the Weavster build-loop engineer (Prompt 3). This repo is the greenfield Go build of
Weavster, a message-oriented integration platform. Implement the MVP module by module, exactly
as specified by the build manifest.

**Read these first, in order:**

1. `AGENTS.md` — repo coding discipline.
2. `agentic-manifest.json` — machine-readable build plan (20 modules; source of truth for structure).
3. `docs/agent-onboarding.md` — language, linters, test/build/container commands, folder layout, ports.
4. `docs/mvp-project-plan.md` — narrative plan + build sequence (P0→P7).

**Hard constraints:** Go >=1.22 · module `github.com/weavster-dev/weavster` · `net/http` + chi ·
wazero · **no CGo** · single static binary · hexagonal ports & adapters (depend on interfaces,
never concrete types across module boundaries) · **no Postgres in tests** (SQLite in-memory) ·
Enterprise items = interface + stub only.

**Execution loop — work the P0→P7 phases in order.** For each module in `agentic-manifest.json`:

1. Create the `files_to_create` at the given `path`, using the given `frameworks_to_use`.
2. Implement to the `acceptance_criteria`, tracing each criterion to a spec use-case.
3. Run the gates: `gofmt -l .` (empty) · `go vet ./...` (clean) · `golangci-lint run` (clean) ·
   `go test -race ./<module>/...` (green).
4. Report per-criterion pass/fail; mark a module complete only when all its gates are green.

**Start now (P0 scaffold):** create `cmd/weavster/main.go` (composition-root stub), add the
manifest's `frameworks_to_use` deps to `go.mod` as you need them, and stub the module packages so
`go build ./...` and `go test ./...` pass. Then continue P1 → P7, committing each phase with a
`CHANGELOG.md` entry.

If the manifest and the specs conflict on a technical fact, stop and ask — never resolve silently.
