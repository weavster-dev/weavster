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
        return cls.model_validate(data)
