---
sidebar_position: 1
---

# Installation

## Requirements

- Rust 1.92.0 or newer
- `wasm32-unknown-unknown` target available through rustup
- Node.js 20 or newer only if you are building the documentation site

Pre-built binaries and hosted install scripts are planned, but the current documented path is source installation.

## Install From Source

```bash
git clone https://github.com/weavster-dev/weavster.git
cd weavster
cargo install --path crates/weavster-cli
```

If the WASM target is missing, install it with:

```bash
rustup target add wasm32-unknown-unknown
```

## Verify Installation

```bash
weavster --version
weavster --help
```

## Development Build

You can also run the CLI directly from the repository:

```bash
cargo run -p weavster-cli -- --help
```

## Next Steps

Once installed, proceed to [Your First Flow](./first-flow) to run the generated file-based example.
