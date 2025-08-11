"""Tests for Weavster settings model."""

from pathlib import Path

from weavster.core.models.settings import WeavsterSettings


def test_weavster_settings_defaults():
    """Test WeavsterSettings uses correct default values."""
    settings = WeavsterSettings()

    assert settings.home == Path.home() / ".weavster"
    assert settings.log_level == "info"


def test_weavster_settings_env_override_home(monkeypatch):
    """Test WeavsterSettings can be overridden by WEAVSTER_HOME environment variable."""
    test_home = "/custom/weavster/home"

    monkeypatch.setenv("WEAVSTER_HOME", test_home)
    settings = WeavsterSettings()

    assert settings.home == Path(test_home)
    assert settings.log_level == "info"  # Should remain default


def test_weavster_settings_env_override_log_level(monkeypatch):
    """Test WeavsterSettings can be overridden by WEAVSTER_LOG_LEVEL environment variable."""
    test_log_level = "debug"

    monkeypatch.setenv("WEAVSTER_LOG_LEVEL", test_log_level)
    settings = WeavsterSettings()

    assert settings.home == Path.home() / ".weavster"  # Should remain default
    assert settings.log_level == test_log_level


def test_weavster_settings_env_override_both(monkeypatch):
    """Test WeavsterSettings can override both settings via environment variables."""
    test_home = "/custom/weavster/home"
    test_log_level = "error"

    monkeypatch.setenv("WEAVSTER_HOME", test_home)
    monkeypatch.setenv("WEAVSTER_LOG_LEVEL", test_log_level)
    settings = WeavsterSettings()

    assert settings.home == Path(test_home)
    assert settings.log_level == test_log_level


def test_weavster_settings_case_insensitive(monkeypatch):
    """Test WeavsterSettings environment variables are case insensitive."""
    test_log_level = "warning"

    # Test lowercase environment variable
    monkeypatch.setenv("weavster_log_level", test_log_level)
    settings = WeavsterSettings()

    assert settings.log_level == test_log_level


def test_weavster_settings_explicit_params():
    """Test WeavsterSettings can be initialized with explicit parameters."""
    test_home = Path("/explicit/home")
    test_log_level = "critical"

    settings = WeavsterSettings(home=test_home, log_level=test_log_level)

    assert settings.home == test_home
    assert settings.log_level == test_log_level


def test_weavster_settings_explicit_overrides_env(monkeypatch):
    """Test explicit parameters override environment variables."""
    env_home = "/env/home"
    env_log_level = "debug"
    explicit_home = Path("/explicit/home")
    explicit_log_level = "error"

    monkeypatch.setenv("WEAVSTER_HOME", env_home)
    monkeypatch.setenv("WEAVSTER_LOG_LEVEL", env_log_level)

    settings = WeavsterSettings(home=explicit_home, log_level=explicit_log_level)

    assert settings.home == explicit_home
    assert settings.log_level == explicit_log_level
