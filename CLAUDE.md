# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Weavster is a cloud-native integration platform that brings declarative simplicity to data pipelines of all sizes. This is a Python package project using modern Python tooling with uv for dependency management.

## Development Environment Setup

```bash
# Install the virtual environment and pre-commit hooks
make install

# This will create a virtual environment using uv and install pre-commit hooks
```

## Common Development Commands

### Code Quality and Linting
```bash
# Run all code quality checks (linting, type checking, dependency checks)
make check

# Run pre-commit hooks manually
uv run pre-commit run -a

# Static type checking with ty or pyright
uv run ty check
uv run pyright

# Check for obsolete dependencies
uv run deptry src

# Format and lint code with ruff
uv run ruff check --fix
uv run ruff format
```

### Testing
```bash
# Run tests with coverage
make test

# Run tests directly with pytest
uv run python -m pytest --cov --cov-config=pyproject.toml --cov-report=xml

# Test across multiple Python versions
tox
```

### Documentation
```bash
# Build and serve documentation locally
make docs

# Test documentation build
make docs-test

# Direct mkdocs commands
uv run mkdocs serve
uv run mkdocs build -s
```

### Build and Release
```bash
# Build wheel file
make build

# Clean build artifacts
make clean-build

# Publish to PyPI (requires PYPI_TOKEN secret)
make publish

# Build and publish in one step
make build-and-publish
```

## Project Structure

- `src/weavster/` - Main source code package
- `tests/` - Test files using pytest
- `docs/` - Documentation files for MkDocs
- `pyproject.toml` - Main configuration file for dependencies, tools, and project metadata
- `Makefile` - Development task automation
- `tox.ini` - Multi-Python version testing configuration

## Code Style and Standards

- **Linting**: Uses ruff with extensive rule set including security (bandit), complexity, and style checks
- **Type Checking**: Uses ty and pyright (both configured for Python 3.9+)
- **Testing**: pytest with coverage reporting
- **Line Length**: 120 characters
- **Python Versions**: Supports 3.9-3.13
- **Documentation**: Google-style docstrings, MkDocs with Material theme

## Important Configuration Notes

- Uses uv for fast Python package management
- Pre-commit hooks are configured and should be run before commits
- Coverage reports are generated in XML format for codecov integration
- Tests automatically run with coverage and doctest modules
- The project uses hatchling as the build backend
