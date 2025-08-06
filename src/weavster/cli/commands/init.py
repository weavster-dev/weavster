"""Init command implementation."""

from pathlib import Path
from typing import Optional

import typer


def is_directory_empty(path: Path, ignore_hidden: bool = True) -> bool:
    """Check if directory is empty, optionally ignoring hidden files."""
    if not path.exists() or not path.is_dir():
        return True

    for item in path.iterdir():
        if ignore_hidden and item.name.startswith("."):
            continue
        return False
    return True


def create_weavster_config(path: Path) -> None:
    """Create a weavster.yml configuration file."""
    config_content = """# Weavster project configuration
# Project names should use lowercase letters and underscores
name: 'my_weavster_project'
version: '1.0.0'

# Profile defines connection and authentication settings
# Create profiles in ~/.weavster/profiles.yml
profile: 'my_weavster_project'

# Directory structure for your integration components
connector-paths: ["connectors"]
route-paths: ["routes"]

# Route configurations
# Routes define how data flows between connectors
routes:
  my_weavster_project:
    # Default settings for all routes in this project
    retry_attempts: 3
    timeout: 300

    # You can organize routes into groups
    ingestion:
      # Settings for data ingestion routes
      batch_size: 1000

    export:
      # Settings for data export routes
      parallel_workers: 4

# Environment configurations
environments:
  development:
    log_level: debug

  production:
    log_level: info
"""

    config_file = path / "weavster.yml"
    config_file.write_text(config_content)


def init_project(project_name: Optional[str] = None) -> None:
    """Initialize a new Weavster project."""
    # Determine target directory
    if project_name:
        target_path = Path.cwd() / project_name
        if target_path.exists():
            typer.secho(f"Error: Directory '{project_name}' already exists.", fg=typer.colors.RED, err=True)
            raise typer.Exit(1)

        # Create the project directory
        target_path.mkdir(parents=True)
        typer.secho(f"Created project directory: {project_name}", fg=typer.colors.GREEN)
    else:
        target_path = Path.cwd()

        # Check if current directory is empty (ignoring hidden files)
        if not is_directory_empty(target_path, ignore_hidden=True):
            typer.secho(
                "Error: Current directory is not empty. Please run this command in an empty directory or use --project to create a new directory.",
                fg=typer.colors.RED,
                err=True,
            )
            raise typer.Exit(1)

    # Create the weavster.yml configuration file
    create_weavster_config(target_path)

    # Success message
    if project_name:
        typer.secho(f"✓ Weavster project '{project_name}' initialized successfully!", fg=typer.colors.GREEN)
        typer.secho(f"  cd {project_name}", fg=typer.colors.BLUE)
        typer.secho("  weavster server start", fg=typer.colors.BLUE)
    else:
        typer.secho("✓ Weavster project initialized successfully!", fg=typer.colors.GREEN)
        typer.secho("  weavster server start", fg=typer.colors.BLUE)
