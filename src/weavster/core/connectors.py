from pathlib import Path
from typing import Any, ClassVar, Union

import yaml

from weavster.core.exceptions.connectors import UnknownConnectorTypeError
from weavster.core.models.connector import ConnectorBaseConfig


class ConnectorRegistry:
    """Registry for connector types and their corresponding classes."""

    _connectors: ClassVar[dict[str, type[ConnectorBaseConfig]]] = {}

    @classmethod
    def register(cls, connector_type: str, connector_class: type[ConnectorBaseConfig]):
        """Register a connector type with its class."""
        cls._connectors[connector_type] = connector_class

    @classmethod
    def create_connector(cls, data: dict[str, Any]) -> ConnectorBaseConfig:
        """Create a connector instance from configuration data."""
        connector_type = data.get("type")
        if connector_type not in cls._connectors:
            available_types = list(cls._connectors.keys())
            raise UnknownConnectorTypeError(str(connector_type), available_types)

        connector_class = cls._connectors[connector_type]
        return connector_class.from_dict(data)

    @classmethod
    def get_registered_types(cls) -> list[str]:
        """Get list of registered connector types."""
        return list(cls._connectors.keys())


class ConnectorLoader:
    """Load connectors from YAML configuration."""

    @staticmethod
    def load_from_yaml(file_path: Union[str, Path]) -> list[ConnectorBaseConfig]:
        """Load connectors from YAML file."""
        with open(file_path) as file:
            config = yaml.safe_load(file)

        connectors = []
        for connector_data in config.get("connectors", []):
            connector = ConnectorRegistry.create_connector(connector_data)
            connectors.append(connector)

        return connectors

    @staticmethod
    def load_from_yaml_string(yaml_content: str) -> list[ConnectorBaseConfig]:
        """Load connectors from YAML string."""
        config = yaml.safe_load(yaml_content)

        connectors = []
        for connector_data in config.get("connectors", []):
            connector = ConnectorRegistry.create_connector(connector_data)
            connectors.append(connector)

        return connectors
