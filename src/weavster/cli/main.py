"""Weavster CLI main entry point."""

from typing import Annotated

import typer

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
