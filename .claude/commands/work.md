---
description: Work on a Linear ticket — research, plan, implement interactively before creating a PR
allowedTools:
  - "Bash(cargo:*)"
  - "Bash(git:*)"
  - "Bash(gh issue:*)"
  - "Bash(gh pr:*)"
  - "Bash(gh api:*)"
  - "mcp__linear-server__get_issue"
  - "mcp__linear-server__update_issue"
  - "mcp__linear-server__list_issue_statuses"
---

# Work on a Linear Ticket

The argument is a Linear ticket identifier (e.g. `WEA-42`).

## Step 1: Fetch the Ticket

Use the Linear MCP tools to fetch the ticket:

1. Call `mcp__linear-server__get_issue` with the identifier from `$ARGUMENTS`
2. Display a summary: title, status, priority, labels, and description

If the Linear MCP server is not available, ask the user to provide the ticket details manually.

## Step 2: Move to "In Progress"

Use `mcp__linear-server__update_issue` to transition the ticket to "In Progress" state.

Confirm the transition to the user.

## Step 3: Understand the Ticket

- Read the ticket description carefully
- Identify affected crates and related code in the repo
- Check for related issues or PRs

## Step 4: Plan the Implementation

Present your implementation plan and **wait for user approval** before writing code:
- Which files need changes
- What the approach is
- Any architectural decisions or trade-offs

This is a collaborative session — the user wants to work WITH you on this.

## Step 5: Implement Iteratively

- Follow CLAUDE.md conventions
- Write tests for new functionality
- Use conventional commits: feat:, fix:, docs:, refactor:, test:, chore:
- Check in with the user at decision points

## Step 6: Create PR (only when told)

Only when the user explicitly says to create the PR:
- Branch name: lowercase the Linear ID and prefix with type, e.g. `feat/wea-42-short-description`
- PR title: `[$ARGUMENTS]: @coderabbitai`
- PR body: `@coderabbitai summary` on the first line, then link the Linear ticket URL
- Do NOT create the PR until explicitly told to

$ARGUMENTS
