"""Weavster settings model."""

from pathlib import Path

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class WeavsterSettings(BaseSettings):
    """Settings for Weavster runtime configuration.

    Settings can be overridden by environment variables prefixed with WEAVSTER_.
    """

    home: Path = Field(
        default=Path.home() / ".weavster", description="Home directory for Weavster configuration and data files"
    )
    log_level: str = Field(
        default="info", description="Logging level for Weavster (debug, info, warning, error, critical)"
    )

    model_config = SettingsConfigDict(
        env_prefix="WEAVSTER_",
        case_sensitive=False,
    )
