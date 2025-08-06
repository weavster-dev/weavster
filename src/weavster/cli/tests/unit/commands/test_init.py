"""Unit tests for init command."""

import tempfile
from pathlib import Path
from unittest.mock import patch

import pytest
import typer

from weavster.cli.commands.init import create_weavster_config, init_project, is_directory_empty


class TestIsDirectoryEmpty:
    """Test directory emptiness validation."""

    def test_nonexistent_directory(self):
        """Test that nonexistent directory is considered empty."""
        path = Path("/nonexistent/path")
        assert is_directory_empty(path) is True

    def test_empty_directory(self):
        """Test that empty directory is considered empty."""
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir)
            assert is_directory_empty(path) is True

    def test_directory_with_regular_files(self):
        """Test that directory with regular files is not empty."""
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir)
            (path / "file.txt").write_text("content")
            assert is_directory_empty(path) is False

    def test_directory_with_hidden_files_ignored(self):
        """Test that directory with only hidden files is considered empty when ignoring hidden files."""
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir)
            (path / ".git").mkdir()
            (path / ".gitignore").write_text("*.pyc")
            assert is_directory_empty(path, ignore_hidden=True) is True

    def test_directory_with_hidden_files_not_ignored(self):
        """Test that directory with hidden files is not empty when not ignoring hidden files."""
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir)
            (path / ".git").mkdir()
            assert is_directory_empty(path, ignore_hidden=False) is False

    def test_directory_with_mixed_files(self):
        """Test directory with both hidden and regular files."""
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir)
            (path / ".git").mkdir()
            (path / "file.txt").write_text("content")
            assert is_directory_empty(path, ignore_hidden=True) is False


class TestCreateWeavsterConfig:
    """Test weavster.yml config file creation."""

    def test_creates_config_file(self):
        """Test that config file is created with correct content."""
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir)
            create_weavster_config(path)

            config_file = path / "weavster.yml"
            assert config_file.exists()

            content = config_file.read_text()
            assert "name: 'my_weavster_project'" in content
            assert "profile: 'my_weavster_project'" in content
            assert "connector-paths:" in content
            assert "route-paths:" in content
            assert "routes:" in content
            assert "environments:" in content
            assert "development:" in content
            assert "production:" in content


class TestInitProject:
    """Test project initialization."""

    def test_init_in_empty_directory(self):
        """Test initializing project in empty directory."""
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir)

            with patch("weavster.cli.commands.init.Path.cwd", return_value=path):
                init_project()

            config_file = path / "weavster.yml"
            assert config_file.exists()

    def test_init_in_non_empty_directory_fails(self):
        """Test that initializing in non-empty directory fails."""
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir)
            (path / "existing_file.txt").write_text("content")

            with patch("weavster.cli.commands.init.Path.cwd", return_value=path):
                with pytest.raises(typer.Exit) as exc_info:
                    init_project()

                assert exc_info.value.exit_code == 1

    def test_init_in_directory_with_hidden_files_succeeds(self):
        """Test that initializing in directory with only hidden files succeeds."""
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir)
            (path / ".git").mkdir()
            (path / ".gitignore").write_text("*.pyc")

            with patch("weavster.cli.commands.init.Path.cwd", return_value=path):
                init_project()

            config_file = path / "weavster.yml"
            assert config_file.exists()

    def test_init_with_project_name_creates_directory(self):
        """Test that using --project creates new directory."""
        with tempfile.TemporaryDirectory() as temp_dir:
            parent_path = Path(temp_dir)

            with patch("weavster.cli.commands.init.Path.cwd", return_value=parent_path):
                init_project(project_name="my-project")

            project_path = parent_path / "my-project"
            assert project_path.exists()
            assert project_path.is_dir()

            config_file = project_path / "weavster.yml"
            assert config_file.exists()

    def test_init_with_existing_project_name_fails(self):
        """Test that using --project with existing directory name fails."""
        with tempfile.TemporaryDirectory() as temp_dir:
            parent_path = Path(temp_dir)
            existing_dir = parent_path / "existing-project"
            existing_dir.mkdir()

            with patch("weavster.cli.commands.init.Path.cwd", return_value=parent_path):
                with pytest.raises(typer.Exit) as exc_info:
                    init_project(project_name="existing-project")

                assert exc_info.value.exit_code == 1
