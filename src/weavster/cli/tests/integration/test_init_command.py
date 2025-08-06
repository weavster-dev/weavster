"""Integration tests for init command."""

import tempfile
from pathlib import Path
from unittest.mock import patch

from typer.testing import CliRunner

from weavster.cli.main import app

runner = CliRunner()


class TestInitCommand:
    """Test init command execution."""

    def test_init_help(self):
        """Test init command help."""
        result = runner.invoke(app, ["init", "--help"])
        assert result.exit_code == 0
        assert "Initialize a new Weavster project" in result.output
        assert "--project" in result.output

    def test_init_in_empty_directory(self):
        """Test init command in empty directory."""
        with (
            tempfile.TemporaryDirectory() as temp_dir,
            patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
        ):
            result = runner.invoke(app, ["init"])
            assert result.exit_code == 0
            assert "initialized successfully" in result.output

            config_file = Path(temp_dir) / "weavster.yml"
            assert config_file.exists()

    def test_init_in_non_empty_directory_fails(self):
        """Test init command fails in non-empty directory."""
        with (
            tempfile.TemporaryDirectory() as temp_dir,
            patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
        ):
            # Create a file to make directory non-empty
            (Path(temp_dir) / "existing_file.txt").write_text("content")

            result = runner.invoke(app, ["init"])
            assert result.exit_code == 1
            assert "Current directory is not empty" in result.output

    def test_init_in_directory_with_hidden_files_succeeds(self):
        """Test init command succeeds in directory with only hidden files."""
        with (
            tempfile.TemporaryDirectory() as temp_dir,
            patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
        ):
            # Create hidden files (like .git directory)
            git_dir = Path(temp_dir) / ".git"
            git_dir.mkdir()
            (Path(temp_dir) / ".gitignore").write_text("*.pyc")

            result = runner.invoke(app, ["init"])
            assert result.exit_code == 0
            assert "initialized successfully" in result.output

            config_file = Path(temp_dir) / "weavster.yml"
            assert config_file.exists()

    def test_init_with_project_option(self):
        """Test init command with --project option."""
        with (
            tempfile.TemporaryDirectory() as temp_dir,
            patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
        ):
            result = runner.invoke(app, ["init", "--project", "my-new-project"])
            assert result.exit_code == 0
            assert "my-new-project" in result.output
            assert "initialized successfully" in result.output

            project_dir = Path(temp_dir) / "my-new-project"
            assert project_dir.exists()
            assert project_dir.is_dir()

            config_file = project_dir / "weavster.yml"
            assert config_file.exists()

    def test_init_with_project_short_option(self):
        """Test init command with -p short option."""
        with (
            tempfile.TemporaryDirectory() as temp_dir,
            patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
        ):
            result = runner.invoke(app, ["init", "-p", "short-project"])
            assert result.exit_code == 0
            assert "short-project" in result.output
            assert "initialized successfully" in result.output

            project_dir = Path(temp_dir) / "short-project"
            assert project_dir.exists()

    def test_init_with_existing_project_name_fails(self):
        """Test init command fails when project directory already exists."""
        with (
            tempfile.TemporaryDirectory() as temp_dir,
            patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
        ):
            # Create existing directory
            existing_dir = Path(temp_dir) / "existing-project"
            existing_dir.mkdir()

            result = runner.invoke(app, ["init", "--project", "existing-project"])
            assert result.exit_code == 1
            assert "already exists" in result.output
