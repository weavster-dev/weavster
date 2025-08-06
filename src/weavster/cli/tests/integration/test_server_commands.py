from unittest.mock import Mock, mock_open, patch

from typer.testing import CliRunner

from weavster.cli.main import app

runner = CliRunner()


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.trio.run")
def test_server_start_foreground(mock_trio_run, mock_is_running):
    """Test server start in foreground mode."""
    mock_is_running.return_value = False

    result = runner.invoke(app, ["server", "start"])

    assert result.exit_code == 0
    mock_is_running.assert_called_once()
    mock_trio_run.assert_called_once()


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.trio.run")
def test_server_start_detached(mock_trio_run, mock_is_running):
    """Test server start in detached mode."""
    mock_is_running.return_value = False

    result = runner.invoke(app, ["server", "start", "--detached"])

    assert result.exit_code == 0
    mock_is_running.assert_called_once()
    mock_trio_run.assert_called_once()


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.trio.run")
def test_server_start_custom_host_port(mock_trio_run, mock_is_running):
    """Test server start with custom host and port."""
    mock_is_running.return_value = False

    result = runner.invoke(app, ["server", "start", "--host", "127.0.0.1", "--port", "3000"])

    assert result.exit_code == 0
    mock_is_running.assert_called_once()
    mock_trio_run.assert_called_once()


@patch("weavster.cli.commands.server.is_server_running")
def test_server_start_already_running(mock_is_running):
    """Test server start when server is already running."""
    mock_is_running.return_value = True

    result = runner.invoke(app, ["server", "start"])

    assert result.exit_code == 1
    assert "Server is already running" in result.output
    mock_is_running.assert_called_once()


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.get_pid_file")
@patch("weavster.cli.commands.server.os.kill")
@patch("weavster.cli.commands.server.remove_pid_file")
def test_server_stop_success(mock_remove_pid, mock_kill, mock_get_pid_file, mock_is_running):
    """Test successful server stop."""
    mock_is_running.return_value = True
    mock_pid_file = Mock()
    mock_pid_file.open = mock_open(read_data="12345")
    mock_get_pid_file.return_value = mock_pid_file

    # Mock the second kill call to raise ProcessLookupError (process stopped)
    mock_kill.side_effect = [None, ProcessLookupError()]

    result = runner.invoke(app, ["server", "stop"])

    assert result.exit_code == 0
    assert "Server stopped" in result.output
    mock_is_running.assert_called_once()
    mock_remove_pid.assert_called_once()


@patch("weavster.cli.commands.server.is_server_running")
def test_server_stop_not_running(mock_is_running):
    """Test server stop when no server is running."""
    mock_is_running.return_value = False

    result = runner.invoke(app, ["server", "stop"])

    assert result.exit_code == 1
    assert "No server is currently running" in result.output
    mock_is_running.assert_called_once()
