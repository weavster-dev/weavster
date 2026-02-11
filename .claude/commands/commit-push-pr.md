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
---

# Commit, Push, and Create PR

Please commit these changes, push to github, and open a pull request.

When creating the pull request, analyze ALL changes between this branch and the base branch (not just the commits you're immediately familiar with). Use `git diff` to review the complete set of changes that will be included in the PR before writing the PR description.

## Linking Issues

If there are related open issues, link them in the PR body:
- `Closes #<issue-number>` - for issues this PR fully resolves
- `Related to #<issue-number>` - for related issues that shouldn't auto-close
