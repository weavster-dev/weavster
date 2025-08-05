from unittest.mock import Mock, mock_open, patch

from weavster.cli.commands.server import is_server_running


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
