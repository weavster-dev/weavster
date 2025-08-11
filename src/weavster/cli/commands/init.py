"""Init command implementation."""

import re
from pathlib import Path
from typing import Optional, Union


class InitResult:
    """Result of project initialization."""

    def __init__(self, success: bool, message: str, suggestions: Optional[list[str]] = None):
        self.success = success
        self.message = message
        self.suggestions = suggestions or []


class ProjectNamePromptResult:
    """Result indicating project name is needed from user."""

    def __init__(self, needs_prompt: bool, message: str, validation_error: Optional[str] = None):
        self.needs_prompt = needs_prompt
        self.message = message
        self.validation_error = validation_error


def validate_project_name(project_name: Optional[str]) -> Optional[str]:
    """Validate project name format.

    Args:
        project_name: The project name to validate

    Returns:
        Error message if invalid, None if valid
    """
    if not project_name or not re.match(r"^[a-zA-Z0-9_]+$", project_name):
        return "Project name must contain only letters, numbers, and underscores"
    return None


def create_weavster_config(path: Path, project_name: str) -> None:
    """Create a weavster.yml configuration file."""
    config_content = f"""# Weavster project configuration
# Project names should use lowercase letters and underscores
name: '{project_name}'
version: '1.0.0'

# Profile defines connection and authentication settings
# Create profiles in ~/.weavster/profiles.yml
profile: '{project_name}'

# Directory structure for your integration components
connector-paths: ["connectors"]
flow-paths: ["flows"]
"""

    config_file = path / "weavster.yml"
    config_file.write_text(config_content)


def prepare_init(project_name: Optional[str] = None) -> Union[ProjectNamePromptResult, InitResult]:
    """Prepare for project initialization, handling prompting if needed.

    Args:
        project_name: Optional project name. If None, indicates prompting is needed.

    Returns:
        ProjectNamePromptResult if project name is needed, or InitResult if validation fails.
    """
    if project_name is None:
        return ProjectNamePromptResult(
            needs_prompt=True, message="Please enter a project name (letters, numbers, and underscores only):"
        )

    # Validate project name
    validation_error = validate_project_name(project_name)
    if validation_error:
        return InitResult(success=False, message=validation_error)

    # Proceed with initialization
    return init_project(project_name)


def init_project(project_name: str) -> InitResult:
    """Initialize a new Weavster project.

    Args:
        project_name: Name of the project directory to create (assumed to be pre-validated).

    Returns:
        InitResult with success status, message, and suggested next steps.
    """
    # Create target directory path
    target_path = Path.cwd() / project_name
    if target_path.exists():
        return InitResult(success=False, message=f"Directory '{project_name}' already exists")

    # Create the project directory
    try:
        target_path.mkdir(parents=True)
    except Exception as e:
        return InitResult(success=False, message=f"Failed to create project directory: {e!s}")

    # Create the weavster.yml configuration file
    try:
        create_weavster_config(target_path, project_name)
    except Exception as e:
        return InitResult(success=False, message=f"Failed to create configuration file: {e!s}")

    # Create default connectors directory with .gitkeep
    try:
        connectors_path = target_path / "connectors"
        connectors_path.mkdir(parents=True)
        gitkeep_file = connectors_path / ".gitkeep"
        gitkeep_file.write_text("")
    except Exception as e:
        return InitResult(success=False, message=f"Failed to create connectors directory: {e!s}")

    # Success
    return InitResult(
        success=True,
        message=f"Weavster project '{project_name}' initialized successfully!",
        suggestions=[f"cd {project_name}", "weavster server start"],
    )
