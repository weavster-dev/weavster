# Contributing to Weavster

Thank you for your interest in contributing to Weavster! This guide explains how to contribute effectively.

## Project Philosophy

Weavster is built with a strong focus on **simplicity and pragmatism**. Before contributing, please understand our core values:

### YAGNI (You Aren't Gonna Need It)

We strongly value keeping things simple:

- **Don't over-engineer** - Solve the problem at hand, not hypothetical future problems
- **No premature abstraction** - Three similar lines of code is better than a premature helper
- **Delete unused code** - Don't keep code "just in case"
- **Minimal dependencies** - Every dependency is a liability
- **Simple > Clever** - Readable, obvious code beats clever tricks

### Code Spirit

- **Focused PRs** - Each PR should do one thing well
- **Tests matter** - New functionality needs tests
- **Docs are part of done** - If you add a feature, document it
- **Error handling** - Use `thiserror` in libraries, `anyhow` in binaries
- **No warnings** - Code must pass `clippy` with zero warnings

## Quick Start

1. **Create an issue first** - Describe what you want to do
2. Fork the repository
3. Create a feature branch from `main`
4. Make your changes
5. Open a pull request linking to the issue

## Issues

We encourage linking PRs to issues when applicable:
- Issues create a discussion space before code is written
- They help us track what's being worked on
- They provide context for reviewers
- They create a searchable history of decisions

How to link:
- Use `Closes #N` in your PR description (auto-closes the issue on merge)
- Use `Part of #N` for PRs that partially address an issue

## Code Review

### For Maintainers (Same-Repo PRs)
Claude Code automatically reviews all PRs from the main repository.

### For External Contributors (Fork PRs)

We use Claude Code for automated reviews, but **we don't run our API tokens on fork PRs** to prevent abuse. You have two options:

#### Option 1: Use Your Own Claude Review (Recommended)

Run Claude reviews on your fork before submitting:

1. Get a Claude Code OAuth token from [claude.ai/code](https://claude.ai/code)
2. Add it as a secret in your fork: **Settings → Secrets → Actions → New secret**
   - Name: `CLAUDE_CODE_OAUTH_TOKEN`
   - Value: Your token
3. Push to your fork - Claude will review automatically
4. Address any feedback before opening a PR to upstream

This gives you fast feedback without waiting for maintainer review.

#### Option 2: Request Maintainer Review

1. Open your PR without Claude review
2. A maintainer will review and add the `safe-to-review` label if appropriate
3. Claude will then run a review using our tokens

**Note:** We only add `safe-to-review` after manually verifying the changes are safe.

## Development Workflow

### Recommended Flow

```
/brainstorm-feature     → Create a plan in /plans/
        ↓
/create-from-plan       → Create GitHub issue(s)
        ↓
Implement               → Write code, tests, docs
        ↓
/commit-push-pr         → Submit for review
```

### Code Standards

- **Rust**: Follow [Rust API Guidelines](https://rust-lang.github.io/api-guidelines/)
- **Formatting**: `cargo fmt` (auto-runs in CI)
- **Linting**: `cargo clippy -- -D warnings` (zero warnings)
- **Tests**: Required for new functionality
- **Docs**: Update relevant docs with your changes

### Commit Messages

Use conventional commits:

```
feat: add new connector for Redis
fix: handle empty input in transform
docs: update CLI reference
refactor: simplify config parsing
test: add integration tests for Kafka
chore: update dependencies
```

## Documentation

Documentation is part of "done" for all work:

- Update relevant docs in `/docs/docs/`
- Add rustdoc comments for public APIs
- Include code examples where helpful
- PR template includes a documentation checklist

## Getting Help

- **Questions**: [GitHub Discussions](https://github.com/weavster-dev/weavster/discussions)
- **Bugs**: [Issue Tracker](https://github.com/weavster-dev/weavster/issues)
- **@claude**: Mention in any issue/PR for AI assistance (maintainers only)

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.
