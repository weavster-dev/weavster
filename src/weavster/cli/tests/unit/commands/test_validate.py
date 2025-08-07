"""Unit tests for validate command."""

import tempfile
from pathlib import Path
from unittest.mock import patch

from weavster.cli.commands.validate import validate_config


def test_validate_valid_config():
    """Test validation of a valid config file."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
route-paths: ['routes']
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        result = validate_config(config_path)
        assert result.success is True
        assert "is valid!" in result.message


def test_validate_missing_file():
    """Test validation fails when file doesn't exist."""
    non_existent_path = Path("/nonexistent/path/weavster.yml")

    result = validate_config(non_existent_path)
    assert result.success is False
    assert "not found" in result.message


def test_validate_empty_file():
    """Test validation fails with empty file."""
    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text("")

        result = validate_config(config_path)
        assert result.success is False
        assert "empty" in result.message


def test_validate_invalid_yaml():
    """Test validation fails with invalid YAML syntax."""
    invalid_yaml = """name: 'test_project'
version: '1.0.0'
profile: [unclosed bracket
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(invalid_yaml)

        result = validate_config(config_path)
        assert result.success is False
        assert "Invalid YAML syntax" in result.message


def test_validate_missing_required_fields():
    """Test validation fails when required fields are missing."""
    invalid_config = """name: 'test_project'
version: '1.0.0'
# Missing profile, connector-paths, route-paths
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(invalid_config)

        result = validate_config(config_path)
        assert result.success is False
        assert "validation failed" in result.message
        assert len(result.errors) > 0
        assert any("profile" in error for error in result.errors)


def test_validate_default_path():
    """Test validation uses current directory weavster.yml when no path provided."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
route-paths: ['routes']
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        with patch("weavster.cli.commands.validate.Path.cwd", return_value=Path(temp_dir)):
            result = validate_config()
            assert result.success is True


def test_validate_allows_extra_fields():
    """Test validation allows extra fields due to extra='allow' in model."""
    config_with_extra = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
route-paths: ['routes']
extra_field: 'this should be allowed'
custom_setting: 42
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(config_with_extra)

        result = validate_config(config_path)
        assert result.success is True
        assert "is valid!" in result.message


def test_validate_generic_exception():
    """Test validate_config handles generic exceptions during YAML processing."""
    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text("name: test")

        # Mock yaml.safe_load to raise a generic exception (not YAMLError)
        with patch("weavster.cli.commands.validate.yaml.safe_load", side_effect=OSError("Disk error")):
            result = validate_config(config_path)

            assert not result.success
            assert "Failed to read configuration file" in result.message
            assert "Disk error" in result.message
