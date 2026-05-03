# Research: 20260503134624-pin-codex-action-nano-model

## Alternatives considered and rejected

### Option A: Leave the Codex model unset

This preserves the current workflow and lets Codex CLI choose its default model.

**Rejected**: The current workflow has no `model` input at `.github/workflows/workflow-quality.yml:80-84`, so behavior can drift when Codex CLI defaults change. The spec requires explicit nano GPT-5 selection.

### Option B: Configure `gpt-5.5-nano`

This would match the user's phrase literally.

**Rejected**: OpenAI's model documentation lists `gpt-5-nano` as the GPT-5 nano alias and does not document a `gpt-5.5-nano` alias. Adding an undocumented model ID risks breaking the action before it can generate changelog entries.

### Option C: Pin Codex action and CLI versions in the same PR

This would make more of the action runtime deterministic.

**Rejected**: The current request is model selection only. The action already supports a model input, and changing action or CLI versions would broaden CI behavior beyond the approved spec.

## Chosen approach — evidence

- `.github/workflows/workflow-quality.yml:80` shows the action is already `openai/codex-action@v1`, so the existing step can be configured in place.
- `.github/workflows/workflow-quality.yml:82-83` shows the action inputs are already grouped under `with`, making a single `model` input the narrowest change.
- `.github/workflows/workflow-quality.yml:62` shows the same-repository and label guards already exist and do not need modification.
- `.github/workflows/workflow-quality.yml:106-119` shows generated changelog commit behavior is separate from model selection and can remain unchanged.
- `https://raw.githubusercontent.com/openai/codex-action/v1/action.yml` documents `model` as an action input.
- `https://platform.openai.com/docs/models/gpt-5-nano/` documents `gpt-5-nano` as the GPT-5 nano model alias.

## Files examined

- `.github/workflows/workflow-quality.yml:59` — identified the auto-generation job to change.
- `.github/workflows/workflow-quality.yml:62` — confirmed existing label and same-repository guard.
- `.github/workflows/workflow-quality.yml:80` — confirmed Codex action version.
- `.github/workflows/workflow-quality.yml:82-84` — confirmed existing action input block and insertion point.
- `.github/workflows/workflow-quality.yml:106-119` — confirmed commit behavior should stay unchanged.
- `.spektacular/plans/20260502165842-workflow-quality-spektacular-alignment/plan.md:32` — prior plan says the Gemini path was replaced by `openai/codex-action@v1`.
- `.spektacular/plans/20260502165842-workflow-quality-spektacular-alignment/research.md:64` — prior research notes `OPENAI_API_KEY` is required for the Codex autofix path.
- `.spektacular/knowledge/conventions.md:11` — repository convention asks for tests for new functionality, but this workflow-only config change has no Rust test surface.

## External references

- `https://raw.githubusercontent.com/openai/codex-action/v1/action.yml` — official action metadata; confirms a `model` input is supported.
- `https://platform.openai.com/docs/models/gpt-5-nano/` — official model page; confirms `gpt-5-nano` is the GPT-5 nano alias.

## Prior plans / specs consulted

- `.spektacular/specs/20260503134624-pin-codex-action-nano-model.md` — defines the current scope and acceptance criteria.
- `.spektacular/plans/20260502165842-workflow-quality-spektacular-alignment/plan.md` — shows the current Codex autofix workflow design.
- `.spektacular/plans/20260502165842-workflow-quality-spektacular-alignment/research.md` — records prior decisions around root changelog generation and OpenAI secret assumptions.

## Open assumptions

The plan assumes `gpt-5-nano` is available to the repository's configured OpenAI API key. If the model is not available to that account, implementation must stop and ask for the desired fallback model.

## Rehydration cues

Re-read `.github/workflows/workflow-quality.yml`, `.spektacular/specs/20260503134624-pin-codex-action-nano-model.md`, and this research file. Verify current action metadata and model docs if time has passed, then inspect the Codex action `with` block near the changelog generator step.
