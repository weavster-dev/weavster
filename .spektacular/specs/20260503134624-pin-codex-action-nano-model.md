# Feature: Pin Codex Action Nano Model

## Overview

The automated changelog generator should use a pinned, cost-efficient Codex model instead of relying on whichever default model the action chooses at runtime. This makes maintainer-triggered changelog autofixes more predictable and cheaper while preserving the existing pull request quality workflow.

## Requirements

- [ ] **Pinned model**
  The changelog autofix automation must explicitly select the nano GPT-5 model.
- [ ] **Existing behavior preserved**
  Pull request changelog checks and generated changelog commits must continue to behave as they do today.
- [ ] **No source-code scope**
  The change must not alter Rust code, generated artifacts, or release-note content.

## Constraints

- Must use an OpenAI model identifier supported by the Codex GitHub Action.
- Must keep the current action version and authentication mechanism unchanged.
- Must follow the repository's Spektacular documentation workflow.

## Acceptance Criteria

- [ ] **Action has explicit model**
  The Codex changelog generation step includes a `model` input with the supported nano GPT-5 model identifier.
- [ ] **Workflow scope unchanged**
  The workflow still checks Rust code changes for release-note evidence and only runs autofix when the existing label and same-repository conditions are met.
- [ ] **No unrelated files changed**
  The implementation changes only the workflow configuration plus required Spektacular artifacts.

## Technical Approach

Update the `openai/codex-action@v1` step in the workflow quality automation to pass `model: gpt-5-nano`. Keep all existing prompt, sandbox, permissions, and commit behavior intact. The OpenAI model documentation lists `gpt-5-nano` as the GPT-5 nano alias; no `gpt-5.5-nano` alias is documented.

## Success Metrics

Maintainer-triggered changelog autofix jobs show the configured model input in the action logs and no longer depend on the Codex CLI default model.

## Non-Goals

- Changing the Codex action version.
- Adding fallback behavior for missing OpenAI secrets.
- Modifying the generated changelog prompt.
