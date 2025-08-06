from unittest.mock import patch

from weavster.cli.commands.version import get_version


@patch("weavster.cli.commands.version.version")
def test_get_version(mock_version):
    mock_version.return_value = "1.2.3"
    result = get_version()
    assert result == "1.2.3"
    mock_version.assert_called_with("weavster")
