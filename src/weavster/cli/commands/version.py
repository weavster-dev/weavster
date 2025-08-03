"""Version command implementation."""

from importlib.metadata import version


def get_version() -> str:
    """Get the current version of Weavster from package metadata."""
    try:
        return version("weavster")
    except Exception:
        return "unknown"
