"""Connector package initialization."""

from weavster.connector.file.connector import FileConnectorConfig
from weavster.core.connectors import ConnectorRegistry


def register_builtin_connectors():
    """Register all built-in connector types."""
    ConnectorRegistry.register("file", FileConnectorConfig)


# Auto-register built-in connectors when module is imported
register_builtin_connectors()
