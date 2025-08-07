import signal
from pathlib import Path
from unittest.mock import Mock, mock_open, patch

import pytest

from weavster.cli.commands.server import (
    get_pid_file,
    is_server_running,
    remove_pid_file,
    run_server_detached,
    run_server_foreground,
    start_server,
    stop_server,
    write_pid_file,
)


@patch("weavster.cli.commands.server.get_pid_file")
def test_is_server_running_no_pid_file(mock_get_pid_file):
    """Test is_server_running when PID file doesn't exist."""
    mock_pid_file = Mock()
    mock_pid_file.exists.return_value = False
    mock_get_pid_file.return_value = mock_pid_file

    result = is_server_running()

    assert result is False


@patch("weavster.cli.commands.server.get_pid_file")
@patch("weavster.cli.commands.server.os.kill")
def test_is_server_running_stale_pid(mock_kill, mock_get_pid_file):
    """Test is_server_running with stale PID file."""
    mock_pid_file = Mock()
    mock_pid_file.exists.return_value = True
    mock_pid_file.open = mock_open(read_data="12345")
    mock_get_pid_file.return_value = mock_pid_file

    # Process doesn't exist
    mock_kill.side_effect = ProcessLookupError()

    result = is_server_running()

    assert result is False
    mock_pid_file.unlink.assert_called_once_with(missing_ok=True)


@patch("weavster.cli.commands.server.get_pid_file")
@patch("weavster.cli.commands.server.os.kill")
def test_is_server_running_active_process(mock_kill, mock_get_pid_file):
    """Test is_server_running with active process."""
    mock_pid_file = Mock()
    mock_pid_file.exists.return_value = True
    mock_pid_file.open = mock_open(read_data="12345")
    mock_get_pid_file.return_value = mock_pid_file

    # Process exists (no exception)
    mock_kill.return_value = None

    result = is_server_running()

    assert result is True


@patch("weavster.cli.commands.server.Path.cwd")
def test_get_pid_file(mock_cwd):
    """Test get_pid_file returns correct path."""
    mock_cwd.return_value = Path("/fake/dir")

    result = get_pid_file()

    assert result == Path("/fake/dir") / ".weavster-server.pid"
    mock_cwd.assert_called_once()


@patch("weavster.cli.commands.server.get_pid_file")
def test_write_pid_file(mock_get_pid_file):
    """Test write_pid_file writes PID to file."""
    mock_pid_file = Mock()
    mock_pid_file.open = mock_open()
    mock_get_pid_file.return_value = mock_pid_file

    write_pid_file(12345)

    mock_pid_file.open.assert_called_once_with("w")
    mock_pid_file.open.return_value.write.assert_called_once_with("12345")


@patch("weavster.cli.commands.server.get_pid_file")
def test_remove_pid_file(mock_get_pid_file):
    """Test remove_pid_file removes PID file."""
    mock_pid_file = Mock()
    mock_get_pid_file.return_value = mock_pid_file

    remove_pid_file()

    mock_pid_file.unlink.assert_called_once_with(missing_ok=True)


@pytest.mark.trio
@patch("weavster.cli.commands.server.trio.lowlevel.open_process")
@patch("weavster.cli.commands.server.write_pid_file")
@patch("weavster.cli.commands.server.remove_pid_file")
async def test_run_server_foreground_success(mock_remove_pid, mock_write_pid, mock_open_process):
    """Test run_server_foreground starts process successfully."""
    mock_process = Mock()
    mock_process.pid = 12345

    # Mock wait() to return a coroutine that resolves to 0
    async def mock_wait():
        return 0

    mock_process.wait = mock_wait
    mock_open_process.return_value = mock_process

    await run_server_foreground("127.0.0.1", 8000)

    mock_open_process.assert_called_once()
    mock_write_pid.assert_called_once_with(12345)
    mock_remove_pid.assert_called_once()


@pytest.mark.trio
@patch("weavster.cli.commands.server.trio.lowlevel.open_process")
@patch("weavster.cli.commands.server.write_pid_file")
@patch("weavster.cli.commands.server.remove_pid_file")
async def test_run_server_foreground_keyboard_interrupt(mock_remove_pid, mock_write_pid, mock_open_process):
    """Test run_server_foreground handles KeyboardInterrupt."""
    mock_process = Mock()
    mock_process.pid = 12345

    wait_call_count = 0

    async def mock_wait():
        nonlocal wait_call_count
        wait_call_count += 1
        if wait_call_count == 1:
            raise KeyboardInterrupt()
        return 0

    mock_process.wait = mock_wait
    mock_process.terminate = Mock()
    mock_open_process.return_value = mock_process

    await run_server_foreground("127.0.0.1", 8000)

    mock_process.terminate.assert_called_once()
    mock_remove_pid.assert_called_once()


@pytest.mark.trio
@patch("weavster.cli.commands.server.trio.lowlevel.open_process")
async def test_run_server_detached(mock_open_process):
    """Test run_server_detached returns process PID."""
    mock_process = Mock()
    mock_process.pid = 12345
    mock_open_process.return_value = mock_process

    result = await run_server_detached("127.0.0.1", 8000)

    assert result == 12345
    mock_open_process.assert_called_once()

    # Verify correct command arguments
    call_args = mock_open_process.call_args[0][0]
    assert "--host" in call_args
    assert "127.0.0.1" in call_args
    assert "--port" in call_args
    assert "8000" in call_args


@pytest.mark.trio
@patch("weavster.cli.commands.server.trio.lowlevel.open_process")
@patch("weavster.cli.commands.server.write_pid_file")
@patch("weavster.cli.commands.server.remove_pid_file")
async def test_run_server_foreground_keyboard_interrupt_no_process(mock_remove_pid, mock_write_pid, mock_open_process):
    """Test run_server_foreground handles KeyboardInterrupt when process is None."""
    # Simulate open_process raising KeyboardInterrupt before process is created
    mock_open_process.side_effect = KeyboardInterrupt()

    await run_server_foreground("127.0.0.1", 8000)

    mock_remove_pid.assert_called_once()


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.trio.run")
def test_start_server_foreground_mode(mock_trio_run, mock_is_running):
    """Test start_server in foreground mode."""
    mock_is_running.return_value = False

    start_server(detached=False, host="127.0.0.1", port=8000)

    mock_trio_run.assert_called_once()
    # Verify trio.run was called with correct function and arguments
    args, kwargs = mock_trio_run.call_args
    assert len(args) == 3  # function, host, port


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.run_server_detached")
@patch("weavster.cli.commands.server.write_pid_file")
def test_start_server_detached_mode_success(mock_write_pid, mock_run_detached, mock_is_running):
    """Test start_server in detached mode returns success result."""
    mock_is_running.return_value = False
    mock_run_detached.return_value = 12345

    result = start_server(detached=True, host="127.0.0.1", port=8000)

    assert result.success
    assert "PID 12345" in result.message
    assert len(result.details) == 2
    assert "http://127.0.0.1:8000" in result.details[0]
    assert "server stop" in result.details[1]


@patch("weavster.cli.commands.server.is_server_running")
def test_start_server_already_running(mock_is_running):
    """Test start_server when server is already running."""
    mock_is_running.return_value = True

    result = start_server()

    assert not result.success
    assert "already running" in result.message


@patch("weavster.cli.commands.server.is_server_running")
def test_stop_server_not_running(mock_is_running):
    """Test stop_server when no server is running."""
    mock_is_running.return_value = False

    result = stop_server()

    assert not result.success
    assert "No server is currently running" in result.message


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.get_pid_file")
@patch("weavster.cli.commands.server.os.kill")
@patch("weavster.cli.commands.server.remove_pid_file")
@patch("time.sleep")
def test_stop_server_force_kill(mock_sleep, mock_remove_pid, mock_kill, mock_get_pid_file, mock_is_running):
    """Test stop_server force kills process that doesn't stop gracefully."""
    mock_is_running.return_value = True
    mock_pid_file = Mock()
    mock_pid_file.open = mock_open(read_data="12345")
    mock_get_pid_file.return_value = mock_pid_file

    # First kill (SIGTERM) succeeds, second kill (check if alive) succeeds, third kill (SIGKILL) succeeds
    mock_kill.side_effect = [None, None, None]

    stop_server()

    assert mock_kill.call_count == 3
    # Verify SIGTERM, then check (signal 0), then SIGKILL
    calls = mock_kill.call_args_list
    assert calls[0][0][1] == signal.SIGTERM
    assert calls[1][0][1] == 0  # Check if process exists
    assert calls[2][0][1] == signal.SIGKILL


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.get_pid_file")
@patch("weavster.cli.commands.server.os.kill")
@patch("weavster.cli.commands.server.remove_pid_file")
@patch("time.sleep")
def test_stop_server_graceful_shutdown(mock_sleep, mock_remove_pid, mock_kill, mock_get_pid_file, mock_is_running):
    """Test stop_server handles graceful shutdown."""
    mock_is_running.return_value = True
    mock_pid_file = Mock()
    mock_pid_file.open = mock_open(read_data="12345")
    mock_get_pid_file.return_value = mock_pid_file

    # First kill (SIGTERM) succeeds, second kill (check if alive) raises ProcessLookupError (process stopped)
    mock_kill.side_effect = [None, ProcessLookupError()]

    stop_server()

    assert mock_kill.call_count == 2
    mock_remove_pid.assert_called_once()


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.get_pid_file")
@patch("weavster.cli.commands.server.remove_pid_file")
def test_stop_server_error_handling(mock_remove_pid, mock_get_pid_file, mock_is_running):
    """Test stop_server error handling for invalid PID file."""
    mock_is_running.return_value = True
    mock_pid_file = Mock()
    mock_pid_file.open = mock_open(read_data="invalid_pid")
    mock_get_pid_file.return_value = mock_pid_file

    result = stop_server()

    assert not result.success
    assert "Error stopping server" in result.message
    mock_remove_pid.assert_called_once()


@pytest.mark.trio
@patch("weavster.cli.commands.server.trio.lowlevel.open_process")
@patch("weavster.cli.commands.server.write_pid_file")
@patch("weavster.cli.commands.server.remove_pid_file")
async def test_run_server_foreground_exception(mock_remove_pid, mock_write_pid, mock_open_process):
    """Test run_server_foreground handles general exceptions."""
    mock_open_process.side_effect = Exception("Something went wrong")

    result = await run_server_foreground("127.0.0.1", 8000)

    assert not result.success
    assert "Failed to start server" in result.message
    assert "Something went wrong" in result.message
    mock_remove_pid.assert_called_once()


@patch("weavster.cli.commands.server.is_server_running")
@patch("weavster.cli.commands.server.run_server_detached")
@patch("weavster.cli.commands.server.write_pid_file")
def test_start_server_detached_exception(mock_write_pid, mock_run_detached, mock_is_running):
    """Test start_server handles exception in detached mode."""
    mock_is_running.return_value = False
    mock_run_detached.side_effect = Exception("Failed to detach")

    result = start_server(detached=True, host="127.0.0.1", port=8000)

    assert not result.success
    assert "Failed to start detached server" in result.message
    assert "Failed to detach" in result.message
