"""Weavster CLI main entry point."""

from typing import Annotated

import typer

from weavster.cli.commands.server import start_server, stop_server
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
def init() -> None:
    """Initialize a new Weavster project in the current directory.

    This command sets up the basic structure and configuration files
    needed to start building data pipelines with Weavster.
    """
    print("Initializing Weavster...")


server_app = typer.Typer(name="server", help="Server management commands")
app.add_typer(server_app, name="server")


@server_app.command("start")
def server_start(
    detached: Annotated[bool, typer.Option("-d", "--detached", help="Run server in background")] = False,
    host: Annotated[str, typer.Option("--host", help="Host to bind to")] = "127.0.0.1",
    port: Annotated[int, typer.Option("--port", help="Port to bind to")] = 8000,
) -> None:
    """Start the Weavster server."""
    start_server(detached=detached, host=host, port=port)


@server_app.command("stop")
def server_stop() -> None:
    """Stop the Weavster server."""
    stop_server()
