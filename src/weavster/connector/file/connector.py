from pathlib import Path

from pydantic import BaseModel, Field

from weavster.core.models.connector import ConnectorBaseConfig


class FileConnectionSettings(BaseModel):
    poll_frequency: int = Field(default=5000, description="Frequency of polling requests in milliseconds")
    directory: Path = Field(description="Directory path to monitor/write files")
    glob_pattern: str = Field(default="*.*", description="Glob pattern for file matching")
    encoding: str = Field(default="utf-8", description="File encoding")


class FileConnectorConfig(ConnectorBaseConfig[FileConnectionSettings]):
    """File connector configuration."""

    type: str = Field(default="file", description="Connector type")
