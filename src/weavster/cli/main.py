"""Weavster CLI main entry point."""

from pathlib import Path
from typing import Annotated, Optional

import typer

from weavster.cli.commands.init import (
    InitResult,
    ProjectNamePromptResult,
    init_project,
    prepare_init,
    validate_project_name,
)
from weavster.cli.commands.server import start_server, stop_server
from weavster.cli.commands.validate import validate_config
from weavster.cli.commands.version import get_version

app = typer.Typer(
    name="weavster",
    help="Weavster - A cloud-native integration platform that brings declarative simplicity to data pipelines of all sizes.",
    no_args_is_help=True,
)


@app.command()
def version(
    short: Annotated[bool, typer.Option("--short", "-s", help="Show only the version number")] = False,
) -> None:
    """Show the version of Weavster."""
    version = get_version()
    if short:
        typer.echo(version)
    else:
        typer.echo(f"Weavster version {version}")


@app.command()
def init(
    project: Annotated[
        Optional[str],
        typer.Option(
            "--project", "-p", help="Create a new directory with the given name and initialize the project there"
        ),
    ] = None,
) -> None:
    """Initialize a new Weavster project.

    If no --project option is provided, you will be prompted to enter a project name.
    The project name should contain only letters, numbers, and underscores.
    """
    # First, check if we need to prompt for project name
    result = prepare_init(project_name=project)

    # Handle prompting workflow
    if isinstance(result, ProjectNamePromptResult) and result.needs_prompt:
        typer.echo(result.message)
        project = typer.prompt("Project name")

        # Validate the prompted project name
        validation_error = validate_project_name(project)
        if validation_error:
            typer.secho(f"Error: {validation_error}", fg=typer.colors.RED, err=True)
            raise typer.Exit(1)

        # Now initialize with the validated project name
        if project:
            result = init_project(project_name=project)
        else:  # pragma: no cover
            # Defensive code: unreachable because validate_project_name() catches empty strings
            typer.secho("Error: No project name provided", fg=typer.colors.RED, err=True)
            raise typer.Exit(1)

    # Handle initialization result (result is now guaranteed to be InitResult)
    if isinstance(result, InitResult):
        if result.success:
            typer.secho(f"Created project directory: {project}", fg=typer.colors.GREEN)
            typer.secho(f"✓ {result.message}", fg=typer.colors.GREEN)
            for suggestion in result.suggestions:
                typer.secho(f"  {suggestion}", fg=typer.colors.BLUE)
        else:
            typer.secho(f"Error: {result.message}", fg=typer.colors.RED, err=True)
            raise typer.Exit(1)
    else:  # pragma: no cover
        # Defensive code: unreachable because prepare_init() has defined return types
        typer.secho("Error: Unexpected result type", fg=typer.colors.RED, err=True)
        raise typer.Exit(1)


@app.command()
def validate(
    config: Annotated[
        Optional[str], typer.Argument(help="Path to weavster.yml config file (defaults to current directory)")
    ] = None,
) -> None:
    """Validate a Weavster configuration file."""

    config_path = Path(config) if config else None
    result = validate_config(config_path)

    if result.success:
        typer.secho(f"✓ {result.message}", fg=typer.colors.GREEN)
    else:
        typer.secho(f"Error: {result.message}", fg=typer.colors.RED, err=True)
        for error in result.errors:
            typer.secho(f"  {error}", fg=typer.colors.RED, err=True)
        raise typer.Exit(1)


server_app = typer.Typer(name="server", help="Server management commands")
app.add_typer(server_app, name="server")


@server_app.command("start")
def server_start(
    detached: Annotated[bool, typer.Option("-d", "--detached", help="Run server in background")] = False,
    host: Annotated[str, typer.Option("--host", help="Host to bind to")] = "127.0.0.1",
    port: Annotated[int, typer.Option("--port", help="Port to bind to")] = 8000,
) -> None:
    """Start the Weavster server."""
    if not detached:
        typer.echo("🚀 Starting Weavster server...")

    result = start_server(detached=detached, host=host, port=port)

    if result.success:
        if detached:
            typer.secho(f"🚀 {result.message}", fg=typer.colors.GREEN)
        else:
            typer.secho(
                f"✅ Server started with PID {result.details[0].split('PID ')[1] if result.details else 'unknown'}",
                fg=typer.colors.GREEN,
            )
            typer.secho(f"🌐 Server running at http://{host}:{port}", fg=typer.colors.GREEN)
            typer.echo("💡 Press Ctrl+C to stop")
        for detail in result.details:
            typer.secho(f"  {detail}", fg=typer.colors.BLUE)
    else:
        typer.secho(f"❌ {result.message}", fg=typer.colors.RED, err=True)
        raise typer.Exit(1)


@server_app.command("stop")
def server_stop() -> None:
    """Stop the Weavster server."""
    result = stop_server()

    if result.success:
        typer.secho(f"✅ {result.message}", fg=typer.colors.GREEN)
        for detail in result.details:
            if "force-terminated" in detail.lower():
                typer.secho(f"⚠️  {detail}", fg=typer.colors.YELLOW)
            else:
                typer.secho(f"  {detail}", fg=typer.colors.BLUE)
    else:
        typer.secho(f"❌ {result.message}", fg=typer.colors.RED, err=True)
        raise typer.Exit(1)
