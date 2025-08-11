"""Unit tests for validate command."""

import tempfile
from pathlib import Path
from unittest.mock import patch

from weavster.cli.commands.validate import validate_config, validate_connector_files


def test_validate_valid_config():
    """Test validation of a valid config file."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
flow-paths: ['flows']
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        # Create connectors directory (empty is valid)
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

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
# Missing profile, connector-paths, flow-paths
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
flow-paths: ['flows']
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        # Create required directories
        (Path(temp_dir) / "connectors").mkdir()
        (Path(temp_dir) / "flows").mkdir()

        with patch("weavster.cli.commands.validate.Path.cwd", return_value=Path(temp_dir)):
            result = validate_config()
            assert result.success is True


def test_validate_allows_extra_fields():
    """Test validation allows extra fields due to extra='allow' in model."""
    config_with_extra = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
flow-paths: ['flows']
extra_field: 'this should be allowed'
custom_setting: 42
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(config_with_extra)

        # Create required directories
        (Path(temp_dir) / "connectors").mkdir()
        (Path(temp_dir) / "flows").mkdir()

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


def test_validate_connector_files_missing_directory():
    """Test connector validation fails when connector directory doesn't exist."""
    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"

        errors = validate_connector_files(config_path, ["nonexistent_connectors"])

        assert len(errors) == 1
        assert "not found" in errors[0]


def test_validate_connector_files_not_directory():
    """Test connector validation fails when connector path is not a directory."""
    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        file_path = Path(temp_dir) / "not_a_directory"
        file_path.write_text("not a directory")

        errors = validate_connector_files(config_path, ["not_a_directory"])

        assert len(errors) == 1
        assert "not a directory" in errors[0]


def test_validate_connector_files_empty_directory():
    """Test connector validation passes with empty connector directory."""
    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        errors = validate_connector_files(config_path, ["connectors"])

        assert len(errors) == 0


def test_validate_connector_files_valid_file_connector():
    """Test connector validation passes with valid file connector."""
    valid_connector_config = """connectors:
  - name: "test_file_connector"
    type: "file"
    direction: "inbound"
    connection_settings:
      directory: "/tmp/test"
      poll_frequency: 1000
      glob_pattern: "*.txt"
      encoding: "utf-8"
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        connector_file = connectors_dir / "file_connector.yml"
        connector_file.write_text(valid_connector_config)

        errors = validate_connector_files(config_path, ["connectors"])

        assert len(errors) == 0


def test_validate_connector_files_invalid_connector_type():
    """Test connector validation fails with unknown connector type."""
    invalid_connector_config = """connectors:
  - name: "test_invalid_connector"
    type: "unknown_type"
    direction: "inbound"
    connection_settings:
      some_setting: "value"
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        connector_file = connectors_dir / "invalid_connector.yml"
        connector_file.write_text(invalid_connector_config)

        errors = validate_connector_files(config_path, ["connectors"])

        assert len(errors) == 1
        assert "Unknown connector type: unknown_type" in errors[0]


def test_validate_connector_files_missing_required_fields():
    """Test connector validation fails with missing required fields."""
    invalid_connector_config = """connectors:
  - type: "file"
    direction: "inbound"
    # Missing name and connection_settings
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        connector_file = connectors_dir / "invalid_connector.yml"
        connector_file.write_text(invalid_connector_config)

        errors = validate_connector_files(config_path, ["connectors"])

        assert len(errors) > 0
        assert any("required" in error.lower() for error in errors)


def test_validate_connector_files_invalid_yaml():
    """Test connector validation fails with invalid YAML syntax."""
    invalid_yaml = """connectors:
  - name: "test_connector"
    type: "file"
    direction: [unclosed bracket
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        connector_file = connectors_dir / "invalid_connector.yml"
        connector_file.write_text(invalid_yaml)

        errors = validate_connector_files(config_path, ["connectors"])

        assert len(errors) == 1
        assert "Invalid YAML syntax" in errors[0]


def test_validate_connector_files_empty_yaml_file():
    """Test connector validation skips empty YAML files."""
    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        # Create empty YAML file
        empty_file = connectors_dir / "empty.yml"
        empty_file.write_text("")

        # Create whitespace-only file
        whitespace_file = connectors_dir / "whitespace.yml"
        whitespace_file.write_text("   \n\t  \n  ")

        errors = validate_connector_files(config_path, ["connectors"])

        assert len(errors) == 0


def test_validate_config_with_connector_validation_errors():
    """Test main validate_config includes connector validation errors."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
flow-paths: ['flows']
"""

    invalid_connector_config = """connectors:
  - name: "test_connector"
    type: "unknown_type"
    direction: "inbound"
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        connector_file = connectors_dir / "invalid_connector.yml"
        connector_file.write_text(invalid_connector_config)

        result = validate_config(config_path)

        assert result.success is False
        assert "validation failed" in result.message
        assert len(result.errors) > 0
        assert any("Unknown connector type: unknown_type" in error for error in result.errors)


def test_validate_config_with_valid_connectors():
    """Test main validate_config passes with valid connector files."""
    valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
flow-paths: ['flows']
"""

    valid_connector_config = """connectors:
  - name: "test_file_connector"
    type: "file"
    direction: "outbound"
    connection_settings:
      directory: "/tmp/output"
      glob_pattern: "*.csv"
"""

    with tempfile.TemporaryDirectory() as temp_dir:
        config_path = Path(temp_dir) / "weavster.yml"
        config_path.write_text(valid_config)

        connectors_dir = Path(temp_dir) / "connectors"
        connectors_dir.mkdir()

        connector_file = connectors_dir / "file_connector.yml"
        connector_file.write_text(valid_connector_config)

        result = validate_config(config_path)

        assert result.success is True
        assert "is valid!" in result.message


def test_read_yaml_file_oserror():
    """Test _read_yaml_file handles OSError when reading files."""
    from weavster.cli.commands.validate import _read_yaml_file

    # Create a file that will cause OSError
    with tempfile.TemporaryDirectory() as temp_dir:
        yaml_file = Path(temp_dir) / "test.yml"
        # Create file but make it unreadable by setting permissions
        yaml_file.write_text("test: value")
        yaml_file.chmod(0o000)

        try:
            content, errors = _read_yaml_file(yaml_file)
            assert content is None
            assert len(errors) == 1
            assert "Error reading" in errors[0]
        finally:
            # Restore permissions so cleanup works
            yaml_file.chmod(0o644)


def test_load_connectors_generic_exception():
    """Test _load_connectors_from_yaml handles generic exceptions."""
    from unittest.mock import patch

    from weavster.cli.commands.validate import _load_connectors_from_yaml

    with tempfile.TemporaryDirectory() as temp_dir:
        yaml_file = Path(temp_dir) / "test.yml"
        yaml_content = "connectors:\n  - name: test\n    type: file"

        # Mock ConnectorLoader to raise a generic exception
        with patch(
            "weavster.cli.commands.validate.ConnectorLoader.load_from_yaml_string",
            side_effect=RuntimeError("Generic error"),
        ):
            connectors, errors = _load_connectors_from_yaml(yaml_file, yaml_content)
            assert connectors is None
            assert len(errors) == 1
            assert "Generic error" in errors[0]


def test_validate_connector_fields_missing_name_and_type():
    """Test _validate_connector_fields with missing name and type fields."""
    from unittest.mock import Mock

    from weavster.cli.commands.validate import _validate_connector_fields

    with tempfile.TemporaryDirectory() as temp_dir:
        yaml_file = Path(temp_dir) / "test.yml"

        # Mock connector with missing name
        connector1 = Mock()
        connector1.name = ""
        connector1.type = "file"

        # Mock connector with missing type
        connector2 = Mock()
        connector2.name = "test"
        connector2.type = ""

        errors = _validate_connector_fields(yaml_file, [connector1, connector2])

        assert len(errors) == 2
        assert "missing required 'name' field" in errors[0]
        assert "missing required 'type' field" in errors[1]
