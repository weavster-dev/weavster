# Research: 20260502165842-workflow-quality-spektacular-alignment

## Alternatives considered and rejected

### Option A: Add `wf-changelog.md` to each failing PR

This would satisfy the old gate for PR #35 and PR #40.

**Rejected**: The repository has moved to Spektacular artifacts and root changelog entries. Adding retired `wf-changelog.md` files would perpetuate the obsolete flow exposed by `.github/workflows/workflow-quality.yml:35`.

### Option B: Delete the Workflow Quality workflow

This would remove the failing gate entirely.

**Rejected**: Removing the whole workflow could disrupt expected check names and would eliminate a useful code-change release-note reminder. Keeping the check but updating its artifact model is lower risk.

### Option C: Update release and nightly workflows in the same PR

Release workflows also contain Gemini references.

**Rejected**: The active PR failure is in the pull request quality workflow. Release automation has a different blast radius and should be reviewed separately.

### Option D: Generate Spektacular artifacts from CI

This would ask automation to create specs and plans when code PRs are missing release-note evidence.

**Rejected**: Spektacular specs and plans are source-of-truth planning artifacts that should be created by the explicit Spektacular workflow, not synthesized after the fact by CI. A generated root changelog entry is a narrower and safer autofix.

## Chosen approach — evidence

The chosen approach updates `.github/workflows/workflow-quality.yml` to recognize current Spektacular/root changelog artifacts.

- `.github/workflows/workflow-quality.yml:35` proves the current gate only accepts `wf-changelog.md`.
- `.github/workflows/workflow-quality.yml:47` proves the failure path is tied to `gemini-autofix`.
- `.github/workflows/workflow-quality.yml:74` and `.github/workflows/workflow-quality.yml:131` prove the original workflow still called Gemini CLI.
- `openai/codex-action@v1` supports running Codex in GitHub Actions with an `OPENAI_API_KEY` secret and a prompt, which fits a maintainer-triggered changelog update path.
- `AGENTS.md:21` and `AGENTS.md:22` define the current `.spektacular/specs/` and `.spektacular/plans/` locations.
- `CHANGELOG.md:5` states the project follows a Spektacular-driven documentation workflow.

## Files examined

- `.github/workflows/workflow-quality.yml:29` — step name and logic still target `wf-changelog.md`.
- `.github/workflows/workflow-quality.yml:47` — fail gate still references the Gemini label.
- `.github/workflows/workflow-quality.yml:60` — auto-generation job is Gemini-specific.
- `.github/workflows/workflow-quality.yml:104` — changelog sync job is also tied to retired artifacts.
- `.github/workflows/ci.yml:1` — core Rust/docs CI is separate and should not change.
- `AGENTS.md:21` — specs live under `.spektacular/specs/`.
- `AGENTS.md:22` — plans live under `.spektacular/plans/`.
- `CHANGELOG.md:5` — root changelog documents Spektacular-driven workflow.

## External references

- `https://github.com/openai/codex-action` — official Codex GitHub Action documentation; confirms `openai/codex-action@v1`, `openai-api-key`, `prompt`, and sandbox inputs.

## Prior plans / specs consulted

- `.spektacular/plans/20260428024252-docs-current-vs-planned-alignment/plan.md` — shows current Spektacular plan/changelog practice.
- `.spektacular/specs/20260502165842-workflow-quality-spektacular-alignment.md` — defines the workflow quality alignment scope.

## Open assumptions

The plan assumes keeping the `Check Workflow Changelog` and `Fail if Missing` job names is safer for branch protection than deleting the jobs outright. If branch protection requires different names, the implementer must stop and ask.

The Codex autofix path assumes the repository has an `OPENAI_API_KEY` secret configured. If that secret is missing, the autofix job will fail and maintainers should add the changelog manually or configure the secret.

## Rehydration cues

Re-read `.github/workflows/workflow-quality.yml`, `AGENTS.md`, and `CHANGELOG.md`, then run:

```bash
rg -n "wf-changelog|wf-update-changelog|gemini-autofix|run-gemini-cli|GEMINI_API_KEY|thoughts/" .github/workflows/workflow-quality.yml
```
