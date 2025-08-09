"""Weavster configuration models."""

from pydantic import BaseModel, ConfigDict, Field


class WeavsterConfig(BaseModel):
    """Main Weavster project configuration."""

    model_config = ConfigDict(populate_by_name=True, extra="allow")

    name: str = Field(description="Project name")
    version: str = Field(default="1.0.0", description="Project version")
    profile: str = Field(description="Profile name for connection settings")

    connector_paths: list[str] = Field(
        alias="connector-paths", description="Directories containing connector definitions"
    )
    route_paths: list[str] = Field(alias="route-paths", description="Directories containing route definitions")
