# CLI Reference

Weavster provides a command-line interface for managing data pipelines and integration workflows.

## Installation

After installing Weavster, the CLI is available as the `weavster` command:

```bash
pip install weavster
```

## Commands

### Main Application

::: weavster.cli.main
    options:
      show_root_heading: false
      show_root_toc_entry: false
      members:
        - app
        - version
        - init

## Usage Examples

### Check Version

Show the current version of Weavster:

```bash
# Full version info
weavster version

# Just the version number
weavster version --short
```

### Initialize Project

Initialize a new Weavster project in the current directory:

```bash
weavster init
```
