# Weavster

**Modern Enterprise Service Bus** - Like dbt, but for real-time transactions.

[![CI](https://github.com/yourusername/weavster/actions/workflows/ci.yml/badge.svg)](https://github.com/yourusername/weavster/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT%2FApache--2.0-blue.svg)](LICENSE)

## What is Weavster?

Weavster is a developer-friendly tool for building real-time data transformation pipelines. It brings the dbt experience to event-driven architectures:

- **YAML Configuration**: Define flows, transforms, and connectors in simple YAML
- **Jinja Templating**: Use familiar Jinja syntax for dynamic configuration
- **Zero-Config Local Dev**: Embedded PostgreSQL - just run `weavster run`
- **Single Binary**: No dependencies to install

## Quick Start

```bash
# Install
curl -fsSL https://weavster.io/install.sh | sh

# Create a project
weavster init my-project
cd my-project

# Run
weavster run
```

## Example Flow

```yaml
# flows/enrich_orders.yaml
name: enrich_orders
input: kafka.orders

transforms:
  - rename:
      customer_id: cust_id

  - add_fields:
      processed_at: "{{ now() }}"

  - compute:
      total_with_tax: "total * 1.08"
      priority: 'if total > 1000 { "high" } else { "normal" }'

  - filter:
      when: "region != 'TEST'"

outputs:
  - postgres.enriched_orders
  - kafka.high_value:
      when: "priority == 'high'"
```

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Input     │────▶│  Transform  │────▶│   Output    │
│  Connector  │     │   Pipeline  │     │  Connector  │
└─────────────┘     └─────────────┘     └─────────────┘
     Kafka              YAML DSL           Postgres
     HTTP               Expressions        Kafka
     File               Filters            HTTP
     Postgres                              File
```

## Features

### Connectors

- **Kafka** - Consume and produce messages
- **PostgreSQL** - Read and write to tables
- **HTTP** - Webhooks and API calls
- **File** - JSON, JSONL, CSV for local development

### Transforms

| Transform | Description |
|-----------|-------------|
| `rename` | Rename fields |
| `add_fields` | Add static or computed fields |
| `compute` | Calculate new values with expressions |
| `filter` | Include/exclude messages |
| `drop_fields` | Remove fields |
| `coalesce` | Use first non-null value |

### Runtime Modes

| Mode | Use Case | Backend |
|------|----------|---------|
| Local | Development | Embedded PostgreSQL |
| Remote | Production | External Postgres + Redis |

## Development

### Prerequisites

- Rust 1.83+

### Building

```bash
# Build all crates
cargo build

# Run tests
cargo test

# Run the CLI
cargo run -p weavster-cli -- --help
```

### Project Structure

```
crates/
├── weavster-core/      # Shared library (config, transforms, connectors)
├── weavster-runtime/   # Execution engine (for Docker)
├── weavster-cli/       # Developer CLI tool
└── weavster-python/    # Python bindings (future)
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Licensed under either of:

- Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE) or http://www.apache.org/licenses/LICENSE-2.0)
- MIT license ([LICENSE-MIT](LICENSE-MIT) or http://opensource.org/licenses/MIT)

at your option.
