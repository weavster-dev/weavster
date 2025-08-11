"""Tests for connector registry and loading."""

import tempfile
from pathlib import Path

import pytest

from weavster.core.connectors import ConnectorLoader, ConnectorRegistry
from weavster.core.exceptions.connectors import UnknownConnectorTypeError
from weavster.core.models.connector import ConnectorBaseConfig


def test_connector_registry_unknown_type_raises_custom_exception():
    """Test that ConnectorRegistry raises UnknownConnectorTypeError for unknown types."""
    # Clear registry for clean test
    ConnectorRegistry._connectors = {}

    data = {"type": "unknown_type", "name": "test", "direction": "inbound"}

    with pytest.raises(UnknownConnectorTypeError) as exc_info:
        ConnectorRegistry.create_connector(data)

    error = exc_info.value
    assert error.connector_type == "unknown_type"
    assert error.available_types == []
    assert "Unknown connector type: unknown_type" in str(error)


def test_connector_registry_unknown_type_with_available_types():
    """Test UnknownConnectorTypeError includes available types when registry has connectors."""
    # Clear registry and add some fake connector types
    ConnectorRegistry._connectors = {
        "file": ConnectorBaseConfig,
        "database": ConnectorBaseConfig,
    }

    data = {"type": "unknown_type", "name": "test", "direction": "inbound"}

    with pytest.raises(UnknownConnectorTypeError) as exc_info:
        ConnectorRegistry.create_connector(data)

    error = exc_info.value
    assert error.connector_type == "unknown_type"
    assert set(error.available_types) == {"file", "database"}
    assert "Available types:" in str(error)
    assert "database" in str(error)
    assert "file" in str(error)


def test_connector_loader_unknown_type_raises_custom_exception():
    """Test that ConnectorLoader propagates UnknownConnectorTypeError."""
    # Clear registry for clean test
    ConnectorRegistry._connectors = {}

    yaml_content = """
connectors:
  - name: "test_connector"
    type: "unknown_type"
    direction: "inbound"
    connection_settings:
      some_setting: "value"
"""

    with pytest.raises(UnknownConnectorTypeError) as exc_info:
        ConnectorLoader.load_from_yaml_string(yaml_content)

    error = exc_info.value
    assert error.connector_type == "unknown_type"


def test_connector_loader_from_yaml_file_unknown_type():
    """Test ConnectorLoader.load_from_yaml with unknown connector type."""
    # Clear registry for clean test
    ConnectorRegistry._connectors = {}

    yaml_content = """
connectors:
  - name: "test_connector"
    type: "unknown_type"
    direction: "inbound"
    connection_settings:
      some_setting: "value"
"""

    with tempfile.NamedTemporaryFile(mode="w", suffix=".yml", delete=False) as f:
        f.write(yaml_content)
        temp_file = Path(f.name)

    try:
        with pytest.raises(UnknownConnectorTypeError) as exc_info:
            ConnectorLoader.load_from_yaml(temp_file)

        error = exc_info.value
        assert error.connector_type == "unknown_type"
    finally:
        temp_file.unlink()  # Clean up temp file


def test_connector_registry_get_registered_types():
    """Test ConnectorRegistry.get_registered_types returns registered connector types."""
    # Clear registry and add test types
    ConnectorRegistry._connectors = {
        "file": ConnectorBaseConfig,
        "database": ConnectorBaseConfig,
        "api": ConnectorBaseConfig,
    }

    registered_types = ConnectorRegistry.get_registered_types()

    assert set(registered_types) == {"file", "database", "api"}


def test_connector_loader_load_from_yaml():
    """Test ConnectorLoader.load_from_yaml loads connectors from file."""
    # Register a connector type for this test
    ConnectorRegistry.register("file", ConnectorBaseConfig)

    yaml_content = """
connectors:
  - name: "test_connector"
    type: "file"
    direction: "inbound"
    connection_settings:
      directory: "/tmp/test"
"""

    with tempfile.NamedTemporaryFile(mode="w", suffix=".yml", delete=False) as f:
        f.write(yaml_content)
        temp_file = Path(f.name)

    try:
        connectors = ConnectorLoader.load_from_yaml(temp_file)

        assert len(connectors) == 1
        assert connectors[0].name == "test_connector"
        assert connectors[0].type == "file"
        assert connectors[0].direction.value == "inbound"
    finally:
        temp_file.unlink()  # Clean up temp file
