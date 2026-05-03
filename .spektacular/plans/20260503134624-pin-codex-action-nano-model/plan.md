# Plan: 20260503134624-pin-codex-action-nano-model

<!-- Metadata -->
<!-- Created: 2026-05-03T13:48:35Z -->
<!-- Commit: fa7fdd6 -->
<!-- Branch: ci-pin-codex-gpt-5-nano -->
<!-- Repository: https://github.com/weavster-dev/weavster.git -->

## Overview

This plan pins the changelog autofix automation to the nano GPT-5 model instead of relying on a moving default. It keeps the existing pull request quality workflow intact while making maintainer-triggered Codex changelog updates cheaper and more predictable.

## Architecture & Design Decisions

The chosen direction is a one-input workflow configuration change: keep the existing Codex action, prompt, sandbox, authentication, permissions, and commit behavior, and add an explicit model selection. This delivers the requested behavior with the smallest blast radius.

The model identifier will be `gpt-5-nano`, because OpenAI's current model documentation lists that as the GPT-5 nano alias and does not document a `gpt-5.5-nano` alias. Using the documented alias avoids introducing an invalid GitHub Actions configuration.

The plan deliberately does not pin the Codex action or Codex CLI package version in the same change. Those are valid follow-ups, but this request is about model selection; combining version pinning would create a broader CI behavior change. See `research.md#alternatives-considered-and-rejected` for rejected options.

## Component Breakdown

- **Pull request quality workflow** owns the release-note evidence gate and the maintainer-triggered changelog autofix path. It remains the only changed component.
- **Codex changelog generator step** owns invoking Codex to update the root changelog when the existing label and same-repository conditions are met. It gains an explicit model input while preserving existing prompt and sandbox behavior.
- **Spektacular plan artifacts** document the approved scope and implementation verification for this workflow-only change.

## Data Structures & Interfaces

No application data structures or public interfaces change. The only contract change is the GitHub Action input set for the Codex step:

```yaml
model: gpt-5-nano
```

The workflow event contract, labels, permissions, and generated changelog commit contract remain unchanged.

## Implementation Detail

The implementation adds a single `model` input beside the existing Codex action inputs. The surrounding YAML should remain stable so reviewers can see that the action still uses the same OpenAI secret, sandbox, and prompt.

No new module, script, helper, or workflow job is introduced. The developer experience for reading the workflow remains the same, with the model now visible in the step configuration and future action logs.

## Dependencies

- **GitHub Actions**: Provides workflow execution and YAML validation; no version change required.
- **OpenAI Codex Action**: Provides the `model` input for `codex exec`; no action version change required.
- **OpenAI model catalog**: Provides the supported `gpt-5-nano` model alias; no repository dependency change required.
- **Prior workflow quality plan**: Established the Codex autofix path and same-repository guard; this plan builds on that behavior without changing it.

## Testing Approach

Testing is static verification for a workflow-only configuration change. The load-bearing assertion is that the Codex action step now has an explicit `model: gpt-5-nano` input while its existing trigger conditions, prompt, sandbox, secret, permissions, and commit behavior remain unchanged.

No Rust unit tests are added because no Rust code or application behavior changes. YAML parsing and targeted text checks cover the changed surface.

## Milestones & Phases

### Milestone 1: Changelog autofix uses the nano model

**What changes**: Maintainer-triggered changelog autofix jobs no longer depend on the Codex CLI default model. They explicitly request the documented nano GPT-5 model while keeping the current workflow behavior.

#### - [x] Phase 1.1: Pin the Codex model

This phase adds the explicit nano GPT-5 model selection to the changelog autofix step. It keeps the current automation shape unchanged so maintainers still use the same label and same-repository guard to request a generated changelog entry.

*Technical detail:* [context.md#phase-11-pin-the-codex-model](./context.md#phase-11-pin-the-codex-model)

**Acceptance criteria**:

- [x] The changelog generator step explicitly selects the documented nano GPT-5 model.
- [x] The workflow still uses the existing OpenAI secret, sandbox, prompt, permissions, label guard, and same-repository guard.
- [x] No Rust source, generated code, or release-note content changes.

## Open Questions

There are no open questions. The undocumented `gpt-5.5-nano` wording from the request was resolved to the documented `gpt-5-nano` alias before implementation.

## Out of Scope

- Changing the Codex action version or Codex CLI package version.
- Adding fallback behavior for a missing OpenAI secret.
- Modifying the generated changelog prompt.
- Changing Rust code, application tests, or release-note content.

## Changelog

### 2026-05-03 — Phase 1.1: Pin the Codex model

**What was done**: The changelog autofix workflow now passes `model: gpt-5-nano` to the Codex action. Existing secret, sandbox, prompt, label guard, same-repository guard, and changelog commit behavior were left unchanged.

**Deviations**: No committed automated test was added because this is a single GitHub Actions input change and the repository has no workflow-test harness; static YAML and action-input checks verified the behavior.

**Files changed**:
- `.github/workflows/workflow-quality.yml`
- `.spektacular/specs/20260503134624-pin-codex-action-nano-model.md`
- `.spektacular/plans/20260503134624-pin-codex-action-nano-model/plan.md`
- `.spektacular/plans/20260503134624-pin-codex-action-nano-model/context.md`
- `.spektacular/plans/20260503134624-pin-codex-action-nano-model/research.md`

**Discoveries**: OpenAI documentation lists `gpt-5-nano` as the supported nano GPT-5 model alias; `gpt-5.5-nano` is not documented.
