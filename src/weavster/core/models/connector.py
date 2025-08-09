import os
import re
from enum import Enum
from typing import Any, Generic, TypeVar

from pydantic import BaseModel, ConfigDict, Field


class ConnectionDirection(Enum):
    """Connection directions."""

    INBOUND = "inbound"
    OUTBOUND = "outbound"


TConnectionSettings = TypeVar("TConnectionSettings", bound=BaseModel)


class ConnectorBaseConfig(BaseModel, Generic[TConnectionSettings]):
    """Base connector configuration with generic connection settings."""

    model_config = ConfigDict(populate_by_name=True, extra="allow")

    name: str = Field(description="Connector name")
    type: str = Field(description="Connector type")
    direction: ConnectionDirection = Field(description="Connector direction")
    connection_settings: TConnectionSettings

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ConnectorBaseConfig":
        """Create connector from dictionary (loaded from YAML)."""
        # Substitute environment variables
        data = cls._substitute_env_vars(data)
        return cls.model_validate(data)

    @staticmethod
    def _substitute_env_vars(obj: Any) -> Any:
        """Recursively substitute environment variables in the format ${VAR_NAME}."""
        if isinstance(obj, dict):
            return {key: ConnectorBaseConfig._substitute_env_vars(value) for key, value in obj.items()}
        elif isinstance(obj, list):
            return [ConnectorBaseConfig._substitute_env_vars(item) for item in obj]
        elif isinstance(obj, str):
            # Replace ${VAR_NAME} with environment variable value
            def replace_env_var(match):
                var_name = match.group(1)
                return os.getenv(var_name, match.group(0))  # Return original if not found

            return re.sub(r"\$\{([^}]+)\}", replace_env_var, obj)
        else:
            return obj
