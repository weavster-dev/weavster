# Feature: CI/CD Final Conversion

## Overview

The delivery automation will be updated so it consistently uses the AI provider already available to the project. This removes reliance on unavailable or retired provider credentials, preventing broken automation and reducing maintenance burden for developers and maintainers.

## Requirements

- [ ] **Automation does not depend on retired AI providers**
  Delivery automation must not require Gemini or Claude credentials to run successfully.

- [ ] **AI-backed automation uses the approved provider**
  Any automation behavior that still needs an LLM call must use the AI provider whose credentials are available to the project.

- [ ] **Existing automation behavior is preserved**
  Non-provider-related behavior in the delivery automation must continue to run as before.

- [ ] **Provider requirements are visible to maintainers**
  Maintainers must be able to tell which AI credential is required for automation that performs LLM-backed work.

## Constraints

- Must use the existing OpenAI API key secret available to GitHub Actions for any required LLM calls.
- Must not require Gemini, Claude, Anthropic, or Google Generative AI secrets for CI/CD automation.
- Must preserve existing workflow triggers, job permissions, and non-AI behavior unless a change is required to remove retired-provider usage.
- Must not expose AI provider secrets in logs, generated artifacts, comments, or pull request output.

## Acceptance Criteria

- [ ] **No retired provider references remain in delivery automation**
  A repository search of delivery automation configuration returns no references to Gemini, Claude, Anthropic, or Google Generative AI credentials, actions, models, or prompts.

- [ ] **LLM-backed automation uses the available project credential**
  Any delivery automation path that performs an LLM-backed task reads its API key from the project’s OpenAI secret and does not require Gemini or Claude secrets.

- [ ] **Automation remains valid after conversion**
  The delivery automation configuration passes the repository’s workflow validation or syntax checks after the provider conversion.

- [ ] **Required AI credential is documented or visible**
  Maintainers can identify the required OpenAI credential from the automation configuration or related project documentation.

## Technical Approach

- Audit `.github/workflows` and any CI/CD helper files invoked by those workflows for Gemini, Claude, Anthropic, and Google Generative AI references.
- Remove or replace provider-specific actions, commands, environment variables, model names, prompts, and secret references tied to Gemini or Claude.
- For workflows that still require an LLM call, use OpenAI with the existing GitHub Actions secret, expected to be `OPENAI_API_KEY` unless the repository already documents a different OpenAI secret name.
- Preserve existing workflow triggers, permissions, labels, comments, artifacts, and non-AI job behavior while changing only the provider integration needed for conversion.
- Validate with repository-wide searches and the available workflow syntax or action linting checks.

## Success Metrics

- Repository CI/CD automation contains zero Gemini, Claude, Anthropic, or Google Generative AI references after delivery.
- AI-backed automation jobs complete using the OpenAI secret in the next representative CI/CD run.
- No CI/CD job fails because a Gemini, Claude, Anthropic, or Google Generative AI credential is missing.

## Non-Goals

- Redesigning the CI/CD pipeline or changing workflow intent beyond the provider conversion is out of scope.
- Adding new AI-assisted automation behavior is out of scope unless it is required to preserve behavior from an existing Gemini or Claude-backed workflow.
- Changing application runtime AI integrations outside CI/CD automation is out of scope.
