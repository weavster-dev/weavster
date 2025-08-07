"""Unit tests for init command."""

import tempfile
from pathlib import Path
from typing import cast
from unittest.mock import patch

from weavster.cli.commands.init import InitResult, create_weavster_config, init_project, prepare_init


def test_create_weavster_config_creates_file():
    """Test that config file is created with correct content."""
    with tempfile.TemporaryDirectory() as temp_dir:
        path = Path(temp_dir)
        project_name = "test_project"
        create_weavster_config(path, project_name)

        config_file = path / "weavster.yml"
        assert config_file.exists()

        content = config_file.read_text()
        assert f"name: '{project_name}'" in content
        assert f"profile: '{project_name}'" in content
        assert "connector-paths:" in content
        assert "route-paths:" in content


def test_init_project_creates_directory():
    """Test initializing project creates new directory."""
    with tempfile.TemporaryDirectory() as temp_dir:
        path = Path(temp_dir)

        with patch("weavster.cli.commands.init.Path.cwd", return_value=path):
            result = init_project("test_project")

        assert result.success
        project_path = path / "test_project"
        assert project_path.exists()
        config_file = project_path / "weavster.yml"
        assert config_file.exists()


def test_init_project_existing_directory_fails():
    """Test that initializing with existing directory name fails."""
    with tempfile.TemporaryDirectory() as temp_dir:
        path = Path(temp_dir)
        existing_dir = path / "existing_project"
        existing_dir.mkdir()

        with patch("weavster.cli.commands.init.Path.cwd", return_value=path):
            result = init_project("existing_project")

        assert not result.success
        assert "already exists" in result.message


def test_init_project_creates_config_with_project_name():
    """Test that config file contains correct project name."""
    with tempfile.TemporaryDirectory() as temp_dir:
        path = Path(temp_dir)
        project_name = "my_test_project"

        with patch("weavster.cli.commands.init.Path.cwd", return_value=path):
            result = init_project(project_name)

        assert result.success
        project_path = path / project_name
        config_file = project_path / "weavster.yml"
        content = config_file.read_text()
        assert f"name: '{project_name}'" in content


def test_init_project_returns_suggestions():
    """Test that successful initialization returns suggestions."""
    with tempfile.TemporaryDirectory() as temp_dir:
        path = Path(temp_dir)
        project_name = "test_project"

        with patch("weavster.cli.commands.init.Path.cwd", return_value=path):
            result = init_project(project_name)

        assert result.success
        assert result.suggestions
        assert any("cd" in suggestion for suggestion in result.suggestions)
        assert any("server" in suggestion for suggestion in result.suggestions)


def test_prepare_init_with_invalid_project_name():
    """Test prepare_init returns InitResult with validation error for invalid project name."""
    result = prepare_init("invalid-name")

    assert not cast(InitResult, result).success
    assert "letters, numbers, and underscores" in result.message


def test_init_project_mkdir_exception():
    """Test init_project handles mkdir exception."""
    with tempfile.TemporaryDirectory() as temp_dir:
        path = Path(temp_dir)

        with (
            patch("weavster.cli.commands.init.Path.cwd", return_value=path),
            patch("pathlib.Path.mkdir", side_effect=OSError("Permission denied")),
        ):
            result = init_project("test_project")

        assert not result.success
        assert "Failed to create project directory" in result.message
        assert "Permission denied" in result.message


def test_init_project_config_creation_exception():
    """Test init_project handles config creation exception."""
    with tempfile.TemporaryDirectory() as temp_dir:
        path = Path(temp_dir)

        with (
            patch("weavster.cli.commands.init.Path.cwd", return_value=path),
            patch("weavster.cli.commands.init.create_weavster_config", side_effect=OSError("Disk full")),
        ):
            result = init_project("test_project")

        assert not result.success
        assert "Failed to create configuration file" in result.message
        assert "Disk full" in result.message
