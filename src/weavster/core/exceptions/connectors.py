"""Connector-related exceptions."""

from typing import Optional


class ConnectorError(Exception):
    """Base exception for connector-related errors."""


class UnknownConnectorTypeError(ConnectorError):
    """Raised when an unknown connector type is requested."""

    def __init__(self, connector_type: str, available_types: Optional[list[str]] = None):
        """Initialize the exception with connector type information.

        Args:
            connector_type: The unknown connector type that was requested
            available_types: Optional list of available connector types
        """
        self.connector_type = connector_type
        self.available_types = available_types or []

        if self.available_types:
            message = (
                f"Unknown connector type: {connector_type}. Available types: {', '.join(sorted(self.available_types))}"
            )
        else:
            message = f"Unknown connector type: {connector_type}"

        super().__init__(message)
