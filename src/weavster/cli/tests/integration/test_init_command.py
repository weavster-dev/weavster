"""Integration tests for init command."""

import tempfile
from pathlib import Path
from unittest.mock import patch

from typer.testing import CliRunner

from weavster.cli.main import app

runner = CliRunner()


def test_init_interactive_prompt():
    """Test init command with interactive prompt."""
    with (
        tempfile.TemporaryDirectory() as temp_dir,
        patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
    ):
        result = runner.invoke(app, ["init"], input="test_project\n")
        assert result.exit_code == 0
        assert "initialized successfully" in result.output

        project_dir = Path(temp_dir) / "test_project"
        assert project_dir.exists()
        config_file = project_dir / "weavster.yml"
        assert config_file.exists()

        # Check that connectors directory and .gitkeep were created
        connectors_dir = project_dir / "connectors"
        assert connectors_dir.exists()
        assert connectors_dir.is_dir()
        gitkeep_file = connectors_dir / ".gitkeep"
        assert gitkeep_file.exists()


def test_init_with_project_option():
    """Test init command with --project option."""
    with (
        tempfile.TemporaryDirectory() as temp_dir,
        patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
    ):
        result = runner.invoke(app, ["init", "--project", "my_new_project"])
        assert result.exit_code == 0
        assert "my_new_project" in result.output
        assert "initialized successfully" in result.output

        project_dir = Path(temp_dir) / "my_new_project"
        assert project_dir.exists()
        assert project_dir.is_dir()

        config_file = project_dir / "weavster.yml"
        assert config_file.exists()

        # Check that connectors directory and .gitkeep were created
        connectors_dir = project_dir / "connectors"
        assert connectors_dir.exists()
        assert connectors_dir.is_dir()
        gitkeep_file = connectors_dir / ".gitkeep"
        assert gitkeep_file.exists()


def test_init_with_project_short_option():
    """Test init command with -p short option."""
    with (
        tempfile.TemporaryDirectory() as temp_dir,
        patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
    ):
        result = runner.invoke(app, ["init", "-p", "short_project"])
        assert result.exit_code == 0
        assert "short_project" in result.output
        assert "initialized successfully" in result.output

        project_dir = Path(temp_dir) / "short_project"
        assert project_dir.exists()

        # Check that connectors directory and .gitkeep were created
        connectors_dir = project_dir / "connectors"
        assert connectors_dir.exists()
        assert connectors_dir.is_dir()
        gitkeep_file = connectors_dir / ".gitkeep"
        assert gitkeep_file.exists()


def test_init_with_existing_project_name_fails():
    """Test init command fails when project directory already exists."""
    with (
        tempfile.TemporaryDirectory() as temp_dir,
        patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
    ):
        # Create existing directory
        existing_dir = Path(temp_dir) / "existing_project"
        existing_dir.mkdir()

        result = runner.invoke(app, ["init", "--project", "existing_project"])
        assert result.exit_code == 1
        assert "already exists" in result.output


def test_init_with_invalid_project_name():
    """Test init command fails with invalid project name."""
    with (
        tempfile.TemporaryDirectory() as temp_dir,
        patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
    ):
        result = runner.invoke(app, ["init"], input="invalid-name-with-dashes\n")
        assert result.exit_code == 1
        assert "letters, numbers, and underscores" in result.output
