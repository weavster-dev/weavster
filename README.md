# Weavster

**Modern Enterprise Service Bus** - Like dbt, but for real-time transactions.

[![CI](https://github.com/weavster-dev/weavster/actions/workflows/ci.yml/badge.svg)](https://github.com/weavster-dev/weavster/actions/workflows/ci.yml)

## What is Weavster?

Weavster is a developer-friendly tool for building real-time data transformation pipelines. It brings the dbt experience to event-driven architectures:

- **YAML Configuration**: Define flows, transforms, and connectors in simple YAML
- **Jinja Templating**: Use familiar Jinja syntax for dynamic configuration
- **Zero-Config Local Dev**: Embedded PostgreSQL - just run `weavster run`
- **Single Binary**: No dependencies to install

## Quick Start

```bash
# Install
curl -fsSL https://weavster.dev/install.sh | sh

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

| Transform     | Description                           |
| ------------- | ------------------------------------- |
| `rename`      | Rename fields                         |
| `add_fields`  | Add static or computed fields         |
| `compute`     | Calculate new values with expressions |
| `filter`      | Include/exclude messages              |
| `drop_fields` | Remove fields                         |
| `coalesce`    | Use first non-null value              |

### Runtime Modes

| Mode   | Use Case    | Backend                   |
| ------ | ----------- | ------------------------- |
| Local  | Development | Embedded PostgreSQL       |
| Remote | Production  | External Postgres + Redis |

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

Weavster is licensed under the [Business Source License 1.1](LICENSE) (BSL 1.1).

**What this means:**

- **Free for most uses**: You can use, modify, and distribute Weavster for any purpose that doesn't compete with our paid offerings
- **Source available**: Full source code is always available
- **Converts to open source**: Each version automatically converts to [MPL 2.0](https://www.mozilla.org/en-US/MPL/2.0/) after 4 years
- **Free products exempt**: Products offered free of charge are never considered competitive

**Not permitted** without a commercial license:

- Offering Weavster as a hosted service that competes with Weavster Dev's paid products
- Embedding Weavster in a competing commercial product

For commercial licensing inquiries, see the [LICENSE](LICENSE) file or contact us.
