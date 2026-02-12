---
description: Create GitHub epic or feature from a plan file
allowedTools:
  - "Bash(gh issue:*)"
  - "Read"
  - "Edit"
  - "Glob"
---

# Create Epic/Feature from Plan

Convert a plan into tracked GitHub issues.

## Workflow

1. **Find the plan**: Look in `/plans/` for the most recent plan, or ask which plan to use if multiple exist.

2. **Read and analyze the plan** to determine scope:

   - **Feature** (small scope): Single cohesive change
     - Creates: 1 issue → expects 1 PR
     - Label: `enhancement`

   - **Epic** (large scope): Multiple distinct tasks
     - Creates: 1 tracking issue + N sub-issues
     - Tracking issue label: `epic`
     - Sub-issues link back with `Part of #<epic>`
     - Each sub-issue → expects 1 PR

3. **Determine scope** by evaluating:
   - Number of distinct tasks/components
   - Affected crates (multi-crate = likely epic)
   - Estimated complexity
   - Natural breakpoints for review

   **Guidelines:**
   - 1-2 related tasks in 1-2 files → Feature
   - 3+ tasks OR multiple crates OR needs phased delivery → Epic

4. **Create GitHub issues**:

   For **Feature**:
   ```bash
   gh issue create --title "Name" --label "enhancement" --body "..."
   ```

   For **Epic**:
   ```bash
   # Create tracking issue first
   gh issue create --title "Name" --label "epic" --body "## Tasks\n- [ ] #TBD - Task 1\n..."

   # Create sub-issues, linking to epic
   gh issue create --title "Task 1" --label "enhancement" --body "Part of #<epic>\n\n..."
   # Update epic with actual issue numbers
   ```

5. **Update the plan file**: Add a header section linking to the created issue(s):

   ```markdown
   ---
   epic: https://github.com/weavster-dev/weavster/issues/N
   # OR
   feature: https://github.com/weavster-dev/weavster/issues/N
   status: planned
   ---
   ```

## Issue Body Template

Include in every issue:

```markdown
## Description
[From plan]

## Tasks
- [ ] Implementation task 1
- [ ] Implementation task 2

## Acceptance Criteria
[From plan or inferred]
```

## After Creation

Report:
- Issue type created (Feature or Epic)
- Links to all created issues
- Confirmation plan was updated
