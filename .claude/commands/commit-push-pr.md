---
description: Commit changes, push to GitHub, and create a pull request
allowedTools:
  - "Bash(git commit:*)"
  - "Bash(git push:*)"
  - "Bash(git add:*)"
  - "Bash(git status:*)"
  - "Bash(git diff:*)"
  - "Bash(git log:*)"
  - "Bash(gh pr:*)"
  - "Bash(gh project:*)"
---

# Commit, Push, and Create PR

Please commit these changes, push to github, and open a pull request.

When creating the pull request, analyze ALL changes between this branch and the base branch (not just the commits you're immediately familiar with). Use `git diff` to review the complete set of changes that will be included in the PR before writing the PR description.

## Linking Issues and Projects

1. **Find related issues**: Check if there are open issues that this PR addresses by running `gh issue list` or searching for relevant keywords.

2. **Link issues in PR body**: Include closing keywords to auto-close issues when the PR merges:
   - `Closes #<issue-number>` - for issues this PR fully resolves
   - `Related to #<issue-number>` - for related issues that shouldn't auto-close

3. **Add PR to project board**: After creating the PR, add it to the Weavster project:
   ```
   gh project item-add 1 --owner weavster-dev --url <PR_URL>
   ```
