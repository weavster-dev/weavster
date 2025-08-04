"""Version command implementation."""

from importlib.metadata import version


def get_version() -> str:
    """Get the current version of Weavster from package metadata."""
    return version("weavster")
