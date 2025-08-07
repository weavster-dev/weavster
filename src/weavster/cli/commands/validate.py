"""Validate command implementation."""

from pathlib import Path
from typing import Optional

import yaml
from pydantic import ValidationError

from weavster.core.models import WeavsterConfig


class ValidationResult:
    """Result of configuration validation."""

    def __init__(self, success: bool, message: str, errors: Optional[list[str]] = None):
        self.success = success
        self.message = message
        self.errors = errors or []


def validate_config(config_path: Optional[Path] = None) -> ValidationResult:
    """Validate a Weavster configuration file.

    Args:
        config_path: Path to configuration file. If None, uses weavster.yml in current directory.

    Returns:
        ValidationResult with success status, message, and any error details.
    """
    # Use current directory's weavster.yml if no path provided
    if config_path is None:
        config_path = Path.cwd() / "weavster.yml"

    # Check if config file exists
    if not config_path.exists():
        return ValidationResult(success=False, message=f"Configuration file not found at {config_path}")

    try:
        # Read and parse YAML file
        with config_path.open() as f:
            config_data = yaml.safe_load(f)

        if config_data is None:
            return ValidationResult(success=False, message="Configuration file is empty")

        # Validate against Pydantic model
        WeavsterConfig.model_validate(config_data)

        # Success
        return ValidationResult(success=True, message=f"Configuration file '{config_path}' is valid!")

    except yaml.YAMLError as e:
        return ValidationResult(success=False, message=f"Invalid YAML syntax in {config_path}: {e!s}")

    except ValidationError as e:
        errors = []
        for error in e.errors():
            field = " -> ".join(str(loc) for loc in error["loc"]) if error["loc"] else "root"
            errors.append(f"{field}: {error['msg']}")

        return ValidationResult(
            success=False, message=f"Configuration validation failed for {config_path}", errors=errors
        )

    except Exception as e:
        return ValidationResult(success=False, message=f"Failed to read configuration file {config_path}: {e!s}")
