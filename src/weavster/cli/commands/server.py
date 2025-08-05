"""Server management commands."""

import os
import signal
import sys
from pathlib import Path
from typing import Annotated

import trio
import typer


def get_pid_file() -> Path:
    """Get the path to the PID file."""
    return Path.cwd() / ".weavster-server.pid"


def is_server_running() -> bool:
    """Check if server is already running."""
    pid_file = get_pid_file()
    if not pid_file.exists():
        return False

    try:
        with pid_file.open() as f:
            pid = int(f.read().strip())
        # Check if process is still running
        os.kill(pid, 0)
    except (ValueError, OSError, ProcessLookupError):
        # PID file is stale, remove it
        pid_file.unlink(missing_ok=True)
        return False
    else:
        return True


def write_pid_file(pid: int) -> None:
    """Write PID to file."""
    pid_file = get_pid_file()
    with pid_file.open("w") as f:
        f.write(str(pid))


def remove_pid_file() -> None:
    """Remove PID file."""
    get_pid_file().unlink(missing_ok=True)


async def run_server_foreground(host: str, port: int) -> None:
    """Run server in foreground with trio subprocess management."""
    process = None
    cmd = [
        sys.executable,
        "-m",
        "granian",
        "--interface",
        "asgi",
        "--host",
        host,
        "--port",
        str(port),
        "weavster.server.app:app",
    ]
    try:
        typer.echo("🚀 Starting Weavster server...")
        process = await trio.lowlevel.open_process(cmd)

        # Write PID for potential cleanup
        write_pid_file(process.pid)
        typer.echo(f"✅ Server started with PID {process.pid}")
        typer.echo(f"🌐 Server running at http://{host}:{port}")
        typer.echo("💡 Press Ctrl+C to stop")

        # Wait for process to complete
        result = await process.wait()
        typer.echo(f"🛑 Server stopped with exit code {result}")

    except KeyboardInterrupt:
        typer.echo("\n🛑 Stopping server...")
        if process:
            process.terminate()
            await process.wait()
        typer.echo("✅ Server stopped")
    finally:
        remove_pid_file()


async def run_server_detached(host: str, port: int) -> int:
    """Run server in detached mode."""
    cmd = [
        sys.executable,
        "-m",
        "granian",
        "--interface",
        "asgi",
        "--host",
        host,
        "--port",
        str(port),
        "weavster.server.app:app",
    ]

    process = await trio.lowlevel.open_process(cmd)
    return process.pid


def start_server(
    detached: Annotated[bool, typer.Option("-d", "--detached", help="Run server in background")] = False,
    host: Annotated[str, typer.Option("--host", help="Host to bind to")] = "127.0.0.1",
    port: Annotated[int, typer.Option("--port", help="Port to bind to")] = 8000,
) -> None:
    """Start the Weavster server."""
    if is_server_running():
        typer.echo("❌ Server is already running. Use 'weavster server stop' to stop it first.")
        raise typer.Exit(1)

    if detached:

        async def _start_detached() -> None:
            pid = await run_server_detached(host, port)
            write_pid_file(pid)
            typer.echo(f"🚀 Server started in background with PID {pid}")
            typer.echo(f"🌐 Server running at http://{host}:{port}")
            typer.echo("💡 Use 'weavster server stop' to stop the server")

        trio.run(_start_detached)
    else:
        trio.run(run_server_foreground, host, port)


def stop_server() -> None:
    """Stop the Weavster server."""
    if not is_server_running():
        typer.echo("❌ No server is currently running.")
        raise typer.Exit(1)

    pid_file = get_pid_file()
    try:
        with pid_file.open() as f:
            pid = int(f.read().strip())

        typer.echo(f"🛑 Stopping server (PID {pid})...")
        os.kill(pid, signal.SIGTERM)

        # Wait a moment for graceful shutdown
        import time

        time.sleep(1)

        # Check if process is still running
        try:
            os.kill(pid, 0)
            typer.echo("⚠️  Server didn't stop gracefully, forcing termination...")
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass  # Process already stopped

        remove_pid_file()
        typer.echo("✅ Server stopped")

    except (ValueError, OSError) as e:
        typer.echo(f"❌ Error stopping server: {e}")
        remove_pid_file()  # Clean up stale PID file
        raise typer.Exit(1) from e
