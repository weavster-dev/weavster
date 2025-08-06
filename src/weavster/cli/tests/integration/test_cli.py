from unittest.mock import patch

from typer.testing import CliRunner

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
    result = runner.invoke(app, ["init"])
    assert result.exit_code == 0
    assert "Initializing Weavster..." in result.output


@patch("weavster.cli.main.start_server")
def test_server_start_integration(mock_start_server):
    """Test server start command integration."""
    result = runner.invoke(app, ["server", "start"])
    assert result.exit_code == 0
    mock_start_server.assert_called_once_with(detached=False, host="127.0.0.1", port=8000)


@patch("weavster.cli.main.start_server")
def test_server_start_with_options(mock_start_server):
    """Test server start command with options."""
    result = runner.invoke(app, ["server", "start", "--detached", "--host", "127.0.0.1", "--port", "3000"])
    assert result.exit_code == 0
    mock_start_server.assert_called_once_with(detached=True, host="127.0.0.1", port=3000)


@patch("weavster.cli.main.stop_server")
def test_server_stop_integration(mock_stop_server):
    """Test server stop command integration."""
    result = runner.invoke(app, ["server", "stop"])
    assert result.exit_code == 0
    mock_stop_server.assert_called_once()
