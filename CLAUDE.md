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
  - `cli/` - Command-line interface module
  - `server/` - FastAPI server module
- `src/weavster/*/tests/` - Test files organized by module
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

## File Creation Guidelines for Claude Code

**CRITICAL: All files must end with a newline character**

When creating or editing files, always ensure:

1. **End files with a newline** - The `fix-end-of-files` pre-commit hook requires this
2. **Use proper line endings** - Files should end with `\n` (newline character)
3. **Avoid "No newline at end of file" messages** - This causes pre-commit failures

### Example of correct file ending:
```
# Last line of content
# ← This blank line ensures proper newline ending
```

### Commands to fix newline issues:
```bash
# Fix all files automatically with pre-commit
uv run pre-commit run fix-end-of-files --all-files

# Or run all pre-commit hooks to fix formatting
uv run pre-commit run --all-files
```

**Always run `make check` or `uv run pre-commit run --all-files` after creating/editing files to ensure compliance with all formatting rules.**

## Testing Guidelines for Claude Code

### Test Organization Structure

**MANDATORY: Always organize tests by type in unit/ and integration/ subfolders**

Tests are organized by module with strict separation by test type:
```
src/weavster/{module}/tests/
├── unit/           # Unit tests - test individual functions and components
│   └── commands/   # (for CLI module) - unit tests for command functions
└── integration/    # Integration tests - test command execution and workflows
```

**Current structure examples:**
- `src/weavster/cli/tests/unit/commands/` - Unit tests for CLI command functions
- `src/weavster/cli/tests/integration/` - Integration tests for CLI command execution
- `src/weavster/server/tests/unit/` - Unit tests for server components

**When creating new tests:**
- **Unit tests** → Always place in `{module}/tests/unit/` subfolder
- **Integration tests** → Always place in `{module}/tests/integration/` subfolder
- **Never** place tests directly in `{module}/tests/` - always use the appropriate subfolder

### Test Writing Standards

**CRITICAL: Never write help text tests**
- Do not create tests that validate `--help` output or help text content
- Focus on functional behavior testing instead

**Unit Test Patterns:**
```python
# Test individual functions with mocking
from unittest.mock import Mock, patch, mock_open

@patch("module.dependency")
def test_function_behavior(mock_dependency):
    """Test specific function behavior with mocked dependencies."""
    mock_dependency.return_value = "expected_value"
    result = function_under_test()
    assert result == "expected_result"
    mock_dependency.assert_called_once()
```

**Integration Test Patterns:**
```python
# Test CLI commands end-to-end
from typer.testing import CliRunner
from myapp.cli.main import app

runner = CliRunner()

def test_command_execution():
    """Test command execution with actual CLI runner."""
    result = runner.invoke(app, ["command", "--option", "value"])
    assert result.exit_code == 0
    assert "expected output" in result.output

def test_server_start_with_host():
    """Use 127.0.0.1 for localhost testing, not 0.0.0.0."""
    result = runner.invoke(app, ["server", "start", "--host", "127.0.0.1", "--port", "3000"])
    assert result.exit_code == 0
```

**FastAPI Test Patterns:**
```python
# Test API endpoints
from fastapi.testclient import TestClient
from myapp.server.app import app

client = TestClient(app)

def test_endpoint_response():
    """Test API endpoint returns correct response."""
    response = client.get("/endpoint")
    assert response.status_code == 200
    data = response.json()
    assert data["field"] == "expected_value"
```

### Test Coverage Requirements

- Aim for high test coverage, especially for core functionality
- **Unit tests** should test individual functions and error conditions → place in `unit/` subfolder
- **Integration tests** should test user-facing command flows → place in `integration/` subfolder
- Use `mock_open` for file operations in tests
- Always mock external dependencies (subprocess, network calls, etc.)

### Test Type Guidelines

**Unit Tests (`unit/` subfolder):**
- Test individual functions in isolation
- Mock all external dependencies
- Test error conditions and edge cases
- Fast execution, no I/O operations

**Integration Tests (`integration/` subfolder):**
- Test command execution end-to-end
- Test CLI workflows and user interactions
- Test API endpoints with TestClient
- May involve multiple components working together

### Running Tests

```bash
# Run all tests
make test

# Run specific test directories
uv run python -m pytest src/weavster/cli/tests/ -v
uv run python -m pytest src/weavster/server/tests/ -v

# Run with coverage for specific modules
uv run python -m pytest src/weavster/cli/tests/ --cov=src/weavster/cli --cov-report=term-missing
```
