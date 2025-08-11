"""Validate command implementation."""

from pathlib import Path
from typing import Optional

import yaml
from pydantic import ValidationError

# Import connector initialization to register all connectors
import weavster.connector  # noqa: F401
from weavster.core.connectors import ConnectorLoader
from weavster.core.exceptions.connectors import UnknownConnectorTypeError
from weavster.core.models import WeavsterConfig


class ValidationResult:
    """Result of configuration validation."""

    def __init__(self, success: bool, message: str, errors: Optional[list[str]] = None):
        self.success = success
        self.message = message
        self.errors = errors or []


def _validate_connector_directory(connector_path: Path) -> list[str]:
    """Validate that a connector directory exists and is valid.

    Args:
        connector_path: Path to the connector directory

    Returns:
        List of validation errors (empty if valid)
    """
    if not connector_path.exists():
        return [f"Connector directory not found: {connector_path}"]

    if not connector_path.is_dir():
        return [f"Connector path is not a directory: {connector_path}"]

    return []


def _read_yaml_file(yaml_file: Path) -> tuple[Optional[str], list[str]]:
    """Read YAML content from file.

    Args:
        yaml_file: Path to the YAML file to read

    Returns:
        Tuple of (yaml_content, errors). Content is None if there were errors or file is empty.
    """
    try:
        with yaml_file.open() as f:
            yaml_content = f.read()
    except OSError as e:
        return None, [f"Error reading {yaml_file}: {e!s}"]

    if not yaml_content.strip():
        return None, []  # Empty file, no errors

    return yaml_content, []


def _load_connectors_from_yaml(yaml_file: Path, yaml_content: str) -> tuple[Optional[list], list[str]]:
    """Load and parse connectors from YAML content.

    Args:
        yaml_file: Path to the YAML file (for error messages)
        yaml_content: YAML content string

    Returns:
        Tuple of (connectors_list, errors). Connectors are None if there were errors.
    """
    try:
        connectors = ConnectorLoader.load_from_yaml_string(yaml_content)
    except yaml.YAMLError as e:
        return None, [f"Invalid YAML syntax in {yaml_file}: {e!s}"]
    except UnknownConnectorTypeError as e:
        return None, [f"Error validating {yaml_file}: {e}"]
    except ValidationError as e:
        errors = []
        for error in e.errors():
            field = " -> ".join(str(loc) for loc in error["loc"]) if error["loc"] else "root"
            errors.append(f"Validation error in {yaml_file}: {field}: {error['msg']}")
        return None, errors
    except Exception as e:
        return None, [f"Error validating {yaml_file}: {e!s}"]
    else:
        return connectors, []


def _validate_connector_fields(yaml_file: Path, connectors: list) -> list[str]:
    """Validate that connectors have required fields.

    Args:
        yaml_file: Path to the YAML file (for error messages)
        connectors: List of connector objects to validate

    Returns:
        List of validation errors (empty if all valid)
    """
    errors = []
    for connector in connectors:
        if not connector.name:
            errors.append(f"Connector in {yaml_file} missing required 'name' field")
        if not connector.type:
            errors.append(f"Connector '{connector.name}' in {yaml_file} missing required 'type' field")
    return errors


def _validate_yaml_file(yaml_file: Path) -> list[str]:
    """Validate a single connector YAML file.

    Args:
        yaml_file: Path to the YAML file to validate

    Returns:
        List of validation errors (empty if valid)
    """
    # Read YAML content
    yaml_content, read_errors = _read_yaml_file(yaml_file)
    if read_errors or yaml_content is None:
        return read_errors

    # Load connectors from YAML
    connectors, load_errors = _load_connectors_from_yaml(yaml_file, yaml_content)
    if load_errors or connectors is None:
        return load_errors

    # Validate connector fields
    return _validate_connector_fields(yaml_file, connectors)


def validate_connector_files(config_path: Path, connector_paths: list[str]) -> list[str]:
    """Validate all connector YAML files in the specified paths.

    Args:
        config_path: Path to the main configuration file (for relative path resolution)
        connector_paths: List of connector directory paths from weavster.yml

    Returns:
        List of validation error messages (empty if all valid)
    """
    errors = []
    config_dir = config_path.parent

    for connector_path_str in connector_paths:
        connector_path = config_dir / connector_path_str

        # Validate directory
        directory_errors = _validate_connector_directory(connector_path)
        if directory_errors:
            errors.extend(directory_errors)
            continue

        # Find and validate all YAML files in the connector directory
        yaml_files = list(connector_path.glob("*.yml")) + list(connector_path.glob("*.yaml"))

        for yaml_file in yaml_files:
            file_errors = _validate_yaml_file(yaml_file)
            errors.extend(file_errors)

    return errors


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
        weavster_config = WeavsterConfig.model_validate(config_data)

        # Validate connector files
        connector_errors = validate_connector_files(config_path, weavster_config.connector_paths)

        if connector_errors:
            return ValidationResult(
                success=False, message=f"Configuration validation failed for {config_path}", errors=connector_errors
            )

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

    except (OSError, PermissionError) as e:
        return ValidationResult(success=False, message=f"Failed to read configuration file {config_path}: {e!s}")
