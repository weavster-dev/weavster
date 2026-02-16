# Linear Ticket Template for Claude Work

Claude uses the Linear ticket body as context for research, planning, and implementation. The template structure matters because Claude parses it to understand scope, constraints, and acceptance criteria.

## Template

```markdown
## Problem
[What problem does this solve? Why does it matter?]

## Desired Outcome
[What should exist when this is done? Be specific about user-facing behavior.]

## Technical Context
- **Affected crates**: [weavster-core, weavster-cli, etc.]
- **Related code**: [Key files, functions, or modules involved]
- **Dependencies**: [Any external crates or services needed]

## Acceptance Criteria
- [ ] [Specific, testable criterion]
- [ ] [Another criterion]
- [ ] Tests pass (unit + integration)
- [ ] No new clippy warnings

## Constraints / Notes
[Any architectural constraints, performance requirements, or things NOT to do]

## References
[Links to docs, related issues, prior art, etc.]
```

## Which Sections Matter Most for Claude

| Section | Why It Matters |
|---------|---------------|
| **Problem** | Claude uses this to research the codebase and understand the "why" |
| **Desired Outcome** | Drives the implementation plan — Claude works backward from this |
| **Affected crates** | Tells Claude where to focus exploration |
| **Acceptance Criteria** | Claude uses these as a checklist and writes tests to match |
| **Constraints** | Prevents Claude from over-engineering or violating architecture rules |

The **Problem** and **Desired Outcome** sections are the most critical. Claude can figure out technical details from the codebase, but it needs clear intent to make good design decisions.
