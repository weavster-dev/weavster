import tempfile
from pathlib import Path
from unittest.mock import patch

from typer.testing import CliRunner

from weavster.cli.commands.server import ServerResult
from weavster.cli.main import app

runner = CliRunner()


@patch("weavster.cli.main.get_version")
def test_version(mock_get_version):
    mock_get_version.return_value = "0.0.0"
    result = runner.invoke(app, ["version"])
    assert result.exit_code == 0
    assert "Weavster version 0.0.0" in result.output


@patch("weavster.cli.main.get_version")
def test_version_short(mock_get_version):
    mock_get_version.return_value = "0.0.0"
    result = runner.invoke(app, ["version", "--short"])
    assert result.exit_code == 0
    assert "Weavster version" not in result.output
    assert "0.0.0" in result.output


def test_initialize():
    """Test init command with project name."""
    with (
        tempfile.TemporaryDirectory() as temp_dir,
        patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
    ):
        result = runner.invoke(app, ["init", "--project", "test_project"])
        assert result.exit_code == 0
        assert "initialized successfully" in result.output


def test_initialize_interactive():
    """Test init command with interactive prompt."""
    with (
        tempfile.TemporaryDirectory() as temp_dir,
        patch("weavster.cli.commands.init.Path.cwd", return_value=Path(temp_dir)),
    ):
        result = runner.invoke(app, ["init"], input="test_project\n")
        assert result.exit_code == 0
        assert "initialized successfully" in result.output


@patch("weavster.cli.main.start_server")
def test_server_start_integration(mock_start_server):
    """Test server start command integration."""

    mock_start_server.return_value = ServerResult(
        True, "Server started", ["Server was running with PID 12345", "Server was at http://127.0.0.1:8000"]
    )
    result = runner.invoke(app, ["server", "start"])
    assert result.exit_code == 0
    mock_start_server.assert_called_once_with(detached=False, host="127.0.0.1", port=8000)


@patch("weavster.cli.main.start_server")
def test_server_start_with_options(mock_start_server):
    """Test server start command with options."""

    mock_start_server.return_value = ServerResult(
        True,
        "Server started in background with PID 12345",
        ["Server running at http://127.0.0.1:3000", "Use 'weavster server stop' to stop the server"],
    )
    result = runner.invoke(app, ["server", "start", "--detached", "--host", "127.0.0.1", "--port", "3000"])
    assert result.exit_code == 0
    mock_start_server.assert_called_once_with(detached=True, host="127.0.0.1", port=3000)


@patch("weavster.cli.main.stop_server")
def test_server_stop_integration(mock_stop_server):
    """Test server stop command integration."""

    mock_stop_server.return_value = ServerResult(True, "Server stopped", ["Server with PID 123 was stopped"])
    result = runner.invoke(app, ["server", "stop"])
    assert result.exit_code == 0
    mock_stop_server.assert_called_once()


@patch("weavster.cli.main.stop_server")
def test_server_stop_force_terminated_warning(mock_stop_server):
    """Test server stop with force-terminated warning."""
    mock_stop_server.return_value = ServerResult(
        True,
        "Server stopped",
        ["Server with PID 123 was stopped", "Server was force-terminated after graceful shutdown timeout"],
    )
    result = runner.invoke(app, ["server", "stop"])
    assert result.exit_code == 0
    assert "force-terminated" in result.output
