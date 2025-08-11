"""Tests for core exceptions."""

from weavster.core.exceptions.connectors import ConnectorError, UnknownConnectorTypeError


def test_connector_error_is_base_exception():
    """Test that ConnectorError is the base exception class."""
    error = ConnectorError("Test message")
    assert isinstance(error, Exception)
    assert str(error) == "Test message"


def test_unknown_connector_type_error_with_type_only():
    """Test UnknownConnectorTypeError with just connector type."""
    error = UnknownConnectorTypeError("unknown_type")

    assert error.connector_type == "unknown_type"
    assert error.available_types == []
    assert str(error) == "Unknown connector type: unknown_type"
    assert isinstance(error, ConnectorError)


def test_unknown_connector_type_error_with_available_types():
    """Test UnknownConnectorTypeError with available types."""
    available_types = ["file", "database", "api"]
    error = UnknownConnectorTypeError("unknown_type", available_types)

    assert error.connector_type == "unknown_type"
    assert error.available_types == available_types
    expected_message = "Unknown connector type: unknown_type. Available types: api, database, file"
    assert str(error) == expected_message


def test_unknown_connector_type_error_with_empty_available_types():
    """Test UnknownConnectorTypeError with empty available types list."""
    error = UnknownConnectorTypeError("unknown_type", [])

    assert error.connector_type == "unknown_type"
    assert error.available_types == []
    assert str(error) == "Unknown connector type: unknown_type"


def test_unknown_connector_type_error_with_single_available_type():
    """Test UnknownConnectorTypeError with single available type."""
    error = UnknownConnectorTypeError("unknown_type", ["file"])

    assert error.connector_type == "unknown_type"
    assert error.available_types == ["file"]
    expected_message = "Unknown connector type: unknown_type. Available types: file"
    assert str(error) == expected_message
