"""Server management commands."""

import os
import signal
import sys
import time
from pathlib import Path
from typing import Optional

import trio


class ServerResult:
    """Result of server operation."""

    def __init__(self, success: bool, message: str, details: Optional[list[str]] = None):
        self.success = success
        self.message = message
        self.details = details or []


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


async def run_server_foreground(host: str, port: int) -> ServerResult:
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
        process = await trio.lowlevel.open_process(cmd)

        # Write PID for potential cleanup
        write_pid_file(process.pid)

        # Wait for process to complete
        result = await process.wait()
        return ServerResult(
            success=True,
            message=f"Server stopped with exit code {result}",
            details=[f"Server was running with PID {process.pid}", f"Server was at http://{host}:{port}"],
        )

    except KeyboardInterrupt:
        if process:
            process.terminate()
            await process.wait()
        return ServerResult(success=True, message="Server stopped by user", details=["Server terminated by Ctrl+C"])
    except Exception as e:
        return ServerResult(success=False, message=f"Failed to start server: {e!s}")
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


def start_server(detached: bool = False, host: str = "127.0.0.1", port: int = 8000) -> ServerResult:
    """Start the Weavster server.

    Args:
        detached: Whether to run server in background
        host: Host to bind to
        port: Port to bind to

    Returns:
        ServerResult with operation status and details
    """
    if is_server_running():
        return ServerResult(
            success=False, message="Server is already running. Use 'weavster server stop' to stop it first"
        )

    if detached:

        async def _start_detached() -> ServerResult:
            try:
                pid = await run_server_detached(host, port)
                write_pid_file(pid)
                return ServerResult(
                    success=True,
                    message=f"Server started in background with PID {pid}",
                    details=[
                        f"Server running at http://{host}:{port}",
                        "Use 'weavster server stop' to stop the server",
                    ],
                )
            except Exception as e:
                return ServerResult(success=False, message=f"Failed to start detached server: {e!s}")

        return trio.run(_start_detached)
    else:
        return trio.run(run_server_foreground, host, port)


def stop_server() -> ServerResult:
    """Stop the Weavster server.

    Returns:
        ServerResult with operation status and details
    """
    if not is_server_running():
        return ServerResult(success=False, message="No server is currently running")

    pid_file = get_pid_file()
    try:
        with pid_file.open() as f:
            pid = int(f.read().strip())

        os.kill(pid, signal.SIGTERM)

        # Wait a moment for graceful shutdown
        time.sleep(1)

        # Check if process is still running
        forced_termination = False
        try:
            os.kill(pid, 0)
            os.kill(pid, signal.SIGKILL)
            forced_termination = True
        except ProcessLookupError:
            pass  # Process already stopped

        remove_pid_file()

        details = [f"Server with PID {pid} was stopped"]
        if forced_termination:
            details.append("Server was force-terminated after graceful shutdown timeout")

        return ServerResult(success=True, message="Server stopped successfully", details=details)

    except (ValueError, OSError) as e:
        remove_pid_file()  # Clean up stale PID file
        return ServerResult(success=False, message=f"Error stopping server: {e!s}")
