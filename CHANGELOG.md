# Changelog

All notable changes to the Weavster project will be documented in this file.

This project follows a Gemini-driven documentation workflow where each feature log in `thoughts/` contributes to this master file.

## [Unreleased]

### Documentation & Cleanup
- **README Cleanup:** Aligned terminology with the codebase (e.g., `rename` -> `map`), updated the `Transforms` table, and clarified connector implementation status.
- **Roadmap:** Added a "Coming Soon" section for the MRK (Mapping, Routing & Keys) feature.
- **Docker Removal:** Removed all Docker-related files and descriptions to simplify the initial project foundation.
- **Gemini Migration:** Migrated all GitHub automation workflows from Claude to Gemini CLI for a consistent developer experience.
- **Quality Enforcement:** Added a new GitHub Action workflow that ensures code changes are documented in `wf-changelog.md` and provides an automated `gemini-autofix` option.
