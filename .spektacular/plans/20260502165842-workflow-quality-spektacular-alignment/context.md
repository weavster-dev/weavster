# Context: 20260502165842-workflow-quality-spektacular-alignment

## Current State Analysis

The failing check on PR #35 and PR #40 is `Workflow Quality / Fail if Missing`. The job fails because `check-changelog` reports code changes without `wf-changelog.md`.

- `.github/workflows/workflow-quality.yml:29` — step name checks for `wf-changelog.md`.
- `.github/workflows/workflow-quality.yml:35` — detection only greps for `wf-changelog.md`.
- `.github/workflows/workflow-quality.yml:39` — warning says the file should be in `thoughts/`.
- `.github/workflows/workflow-quality.yml:47` — failure is skipped only when the `gemini-autofix` label is present.
- `.github/workflows/workflow-quality.yml:52` — failure message says code PRs are missing `wf-changelog.md`.
- `.github/workflows/workflow-quality.yml:53` — failure message points to `/wf-update-changelog`.
- `.github/workflows/workflow-quality.yml:60` through `.github/workflows/workflow-quality.yml:95` — auto-generation job calls Gemini CLI and writes retired `thoughts/` artifacts.
- `.github/workflows/workflow-quality.yml:118` through `.github/workflows/workflow-quality.yml:153` — sync job also depends on `wf-changelog.md` and Gemini CLI.
- `AGENTS.md:21` and `AGENTS.md:22` — current workflow stores specs and plans under `.spektacular/`.

## Per-Phase Technical Notes

### Phase 1.1: Replace retired changelog gate

- `.github/workflows/workflow-quality.yml:29` — rename the step to check for current release-note artifacts rather than `wf-changelog.md`.
- `.github/workflows/workflow-quality.yml:35` — replace `wf-changelog.md` detection with a diff check for `CHANGELOG.md`, `.spektacular/specs/`, or `.spektacular/plans/`.
- `.github/workflows/workflow-quality.yml:39` — update the warning to mention root changelog or Spektacular artifacts.
- `.github/workflows/workflow-quality.yml:47` — replace the `gemini-autofix` label condition with a `codex-autofix` same-repository guard.
- `.github/workflows/workflow-quality.yml:52` through `.github/workflows/workflow-quality.yml:54` — update the failure message to current Spektacular wording.
- `.github/workflows/workflow-quality.yml:58` through `.github/workflows/workflow-quality.yml:105` — replace the Gemini auto-generation job with `openai/codex-action@v1`, scoped to same-repository PRs labeled `codex-autofix`.
- `.github/workflows/workflow-quality.yml:106` onward — keep the sync job as a no-op status compatibility job because Codex updates the root changelog directly.

**Complexity**: Low
**Token estimate**: ~8k
**Agent strategy**: Single agent, sequential execution

## Testing Strategy

Validate the workflow YAML syntax and run static greps to ensure the PR quality workflow no longer references Gemini, `wf-changelog.md`, `/wf-update-changelog`, or `thoughts/`.

For behavior, use local git-diff simulations against current `main` to confirm:

- a code-only diff would set `needs_changelog=true`;
- a code diff with `CHANGELOG.md` would set `needs_changelog=false`;
- a code diff with `.spektacular/specs/**` or `.spektacular/plans/**` would set `needs_changelog=false`.

Static review should also verify that the Codex autofix job:

- requires the `codex-autofix` label;
- only runs for PR branches in the same repository;
- instructs Codex to update only `CHANGELOG.md`;
- commits only `CHANGELOG.md`.

## Project References

- `.spektacular/specs/20260502165842-workflow-quality-spektacular-alignment.md` — approved workflow-fix scope.
- `.spektacular/plans/20260428024252-docs-current-vs-planned-alignment/plan.md` — prior plan establishing Spektacular changelog practice.
- `https://github.com/weavster-dev/weavster/pull/35` — currently failing `Fail if Missing`.
- `https://github.com/weavster-dev/weavster/pull/40` — currently failing `Fail if Missing`.

## Token Management Strategy

| Tier | Token Budget | Agent Strategy |
|------|-------------|----------------|
| Low | ~10k | Single agent, sequential |
| Medium | ~25k | 2-3 parallel agents |
| High | ~50k+ | Parallel analysis, sequential integration |

This is low-tier because it changes one workflow file plus Spektacular/release-note artifacts.

## Migration Notes

After this PR merges, open PR branches may need to rebase or merge `main` to pick up the updated pull request workflow.

## Performance Considerations

No runtime performance impact. The workflow should avoid old Gemini jobs on normal PRs; `codex-autofix` adds work only when explicitly requested by label.
