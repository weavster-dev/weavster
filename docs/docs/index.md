---
sidebar_position: 1
---

# Introduction

Weavster is a modern Enterprise Service Bus - like dbt but for real-time transactions.

## What is Weavster?

Weavster is a developer-friendly tool for building real-time data transformation pipelines:

- **dbt-like DX** - YAML configuration, Jinja templating, simple CLI
- **Real-time focus** - FIFO queues, not batch processing
- **Zero-config local dev** - Embedded PostgreSQL, single binary distribution
- **Safe transforms** - WASM sandboxing for untrusted code

## Quick Start

```bash
# Install weavster
cargo install weavster-cli

# Initialize a new project
weavster init my-project
cd my-project

# Run locally with embedded Postgres
weavster run
```

## Core Concepts

- **Project** - Collection of flows organized in a directory with `weavster.yaml`
- **Flow** - 1 input → N outputs with transforms in between
- **Connector** - Input/Output adapter (Kafka, Postgres, HTTP, File, etc.)
- **Transform** - Data manipulation (rename, filter, compute, etc.)

## Next Steps

- [Installation](./getting-started/installation) - Get Weavster installed
- [Your First Flow](./getting-started/first-flow) - Create your first data flow
- [Configuration](./configuration/project) - Learn about project configuration
