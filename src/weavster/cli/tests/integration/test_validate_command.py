"""Integration tests for validate command."""

import tempfile
from pathlib import Path
from unittest.mock import patch

from typer.testing import CliRunner

from weavster.cli.main import app

runner = CliRunner()


def test_validate_valid_config():
    """Test validation of valid config file."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
route-paths: ['routes']
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        # Create empty connectors directory
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        result = runner.invoke(app, ["validate", str(config_path)])
        assert result.exit_code == 0
        assert "is valid!" in result.output


def test_validate_invalid_config():
    """Test validation of invalid config file."""
    invalid_config = """name: 'test_project'
version: '1.0.0'
# Missing required fields
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(invalid_config)

        result = runner.invoke(app, ["validate", str(config_path)])
        assert result.exit_code == 1
        assert "Configuration validation failed" in result.output
        assert "profile: Field required" in result.output
        assert "connector-paths: Field required" in result.output
        assert "route-paths: Field required" in result.output


def test_validate_nonexistent_file():
    """Test validation fails with non-existent file."""
    result = runner.invoke(app, ["validate", "nonexistent.yml"])
    assert result.exit_code == 1
    assert "Configuration file not found" in result.output


def test_validate_invalid_yaml():
    """Test validation fails with invalid YAML."""
    invalid_yaml = """name: 'test_project'
version: '1.0.0'
profile: [unclosed bracket
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(invalid_yaml)

        result = runner.invoke(app, ["validate", str(config_path)])
        assert result.exit_code == 1
        assert "Invalid YAML syntax" in result.output


def test_validate_default_config_path():
    """Test validation uses default weavster.yml in current directory."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
route-paths: ['routes']
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        # Create empty connectors directory
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        # Mock the current working directory
        with patch("weavster.cli.commands.validate.Path.cwd", return_value=Path(temp_dir)):
            result = runner.invoke(app, ["validate"])
            assert result.exit_code == 0
            assert "is valid!" in result.output


def test_validate_with_valid_file_connector():
    """Test validation passes with valid file connector configuration."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
route-paths: ['routes']
"""

    valid_file_connector = """connectors:
  - name: "test_file_connector"
    type: "file"
    direction: "inbound"
    connection_settings:
      directory: "/tmp/input"
      poll_frequency: 5000
      glob_pattern: "*.txt"
      encoding: "utf-8"
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        # Create connectors directory with valid connector file
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        connector_file = connectors_dir / "file_connector.yml"
        connector_file.write_text(valid_file_connector)

        result = runner.invoke(app, ["validate", str(config_path)])
        assert result.exit_code == 0
        assert "is valid!" in result.output


def test_validate_with_invalid_connector_type():
    """Test validation fails with invalid connector type."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
route-paths: ['routes']
"""

    invalid_connector = """connectors:
  - name: "test_invalid_connector"
    type: "unknown_connector_type"
    direction: "inbound"
    connection_settings:
      some_setting: "value"
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        # Create connectors directory with invalid connector file
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        connector_file = connectors_dir / "invalid_connector.yml"
        connector_file.write_text(invalid_connector)

        result = runner.invoke(app, ["validate", str(config_path)])
        assert result.exit_code == 1
        assert "Configuration validation failed" in result.output
        assert "Unknown connector type: unknown_connector_type" in result.output


def test_validate_with_missing_connector_directory():
    """Test validation fails when connector directory doesn't exist."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['nonexistent_connectors']
route-paths: ['routes']
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        result = runner.invoke(app, ["validate", str(config_path)])
        assert result.exit_code == 1
        assert "Configuration validation failed" in result.output
        assert "not found" in result.output
