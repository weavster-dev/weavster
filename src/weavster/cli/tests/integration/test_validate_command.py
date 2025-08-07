"""Integration tests for validate command."""

import tempfile
from pathlib import Path
from unittest.mock import patch

from typer.testing import CliRunner

from weavster.cli.main import app

runner = CliRunner()


class TestValidateCommand:
    """Test validate command execution."""

    def test_validate_help(self):
        """Test validate command help."""
        result = runner.invoke(app, ["validate", "--help"])
        assert result.exit_code == 0
        assert "Validate a Weavster configuration file" in result.output

    def test_validate_valid_config(self):
        """Test validation of valid config file."""
        valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
route-paths: ['routes']
"""

        with tempfile.TemporaryDirectory() as temp_dir:
            config_path = Path(temp_dir) / "weavster.yml"
            config_path.write_text(valid_config)

            result = runner.invoke(app, ["validate", str(config_path)])
            assert result.exit_code == 0
            assert "is valid!" in result.output

    def test_validate_invalid_config(self):
        """Test validation of invalid config file."""
        invalid_config = """name: 'test_project'
version: '1.0.0'
# Missing required fields
"""

        with tempfile.TemporaryDirectory() as temp_dir:
            config_path = Path(temp_dir) / "weavster.yml"
            config_path.write_text(invalid_config)

            result = runner.invoke(app, ["validate", str(config_path)])
            assert result.exit_code == 1
            assert "Configuration validation failed" in result.output
            assert "profile: Field required" in result.output
            assert "connector-paths: Field required" in result.output
            assert "route-paths: Field required" in result.output

    def test_validate_nonexistent_file(self):
        """Test validation fails with non-existent file."""
        result = runner.invoke(app, ["validate", "nonexistent.yml"])
        assert result.exit_code == 1
        assert "Configuration file not found" in result.output

    def test_validate_invalid_yaml(self):
        """Test validation fails with invalid YAML."""
        invalid_yaml = """name: 'test_project'
version: '1.0.0'
profile: [unclosed bracket
"""

        with tempfile.TemporaryDirectory() as temp_dir:
            config_path = Path(temp_dir) / "weavster.yml"
            config_path.write_text(invalid_yaml)

            result = runner.invoke(app, ["validate", str(config_path)])
            assert result.exit_code == 1
            assert "Invalid YAML syntax" in result.output

    def test_validate_default_config_path(self):
        """Test validation uses default weavster.yml in current directory."""
        valid_config = """name: 'test_project'
version: '1.0.0'
profile: 'test_project'
connector-paths: ['connectors']
route-paths: ['routes']
"""

        with tempfile.TemporaryDirectory() as temp_dir:
            config_path = Path(temp_dir) / "weavster.yml"
            config_path.write_text(valid_config)

            # Mock the current working directory
            with patch("weavster.cli.commands.validate.Path.cwd", return_value=Path(temp_dir)):
                result = runner.invoke(app, ["validate"])
                assert result.exit_code == 0
                assert "is valid!" in result.output
