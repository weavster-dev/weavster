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
def test_version__short(mock_get_version):
    mock_get_version.return_value = "0.0.0"
    result = runner.invoke(app, ["version", "--short"])
    assert result.exit_code == 0
    assert "Weavster version" not in result.output
    assert "0.0.0" in result.output


def test_initialize():
    result = runner.invoke(app, ["init"])
    assert result.exit_code == 0
    assert "Initializing Weavster..." in result.output
