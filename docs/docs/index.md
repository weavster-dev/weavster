---
sidebar_position: 1
---

# Introduction

Weavster is an early MVP for defining YAML data flows and running local file-based JSONL transformations through generated WASM.

## Current Status

The current reliable path is:

- Create a project with `weavster init`
- Run the generated file-to-file JSONL flow
- Use connector references such as `file.input` and `file.output`
- Store local runtime state in SQLite under `.weavster/data/local.db`

Several broader integration features are present as configuration models, placeholders, or roadmap items. They are not current end-to-end runtime guarantees.

## Status Labels

| Label | Meaning |
| --- | --- |
| Current | Usable today in the documented workflow |
| Partial | Implemented in some layers but limited or incomplete |
| Config-only | Parsed or modeled, but not executed end-to-end |
| Placeholder | Command or surface exists but does not provide useful behavior yet |
| Planned | Roadmap item with no current usable behavior |

## Quick Start

```bash
git clone https://github.com/weavster-dev/weavster.git
cd weavster
cargo install --path crates/weavster-cli

weavster init my-project
cd my-project
weavster run
```

## Core Concepts

- **Project**: A directory with `weavster.yaml`, flow files, connector files, and local data.
- **Flow**: One connector reference for input, an ordered transform list, and one or more output references.
- **Connector**: A named adapter configuration. File connectors are the current runtime-supported path.
- **Transform**: A YAML operation compiled into the WASM flow path, with support varying by transform.

## Next Steps

- [Installation](./getting-started/installation) - Install from source
- [Your First Flow](./getting-started/first-flow) - Run the generated file flow
- [Configuration](./configuration/project) - Understand current config shape
- [Commands](./cli/commands) - See implemented, partial, placeholder, and planned CLI surfaces
