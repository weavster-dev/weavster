# Weavster Development Makefile
# Run `make help` to see available commands

.PHONY: help build build-release test lint fmt check clean install run dev setup pre-commit

# Default target
help:
	@echo "Weavster Development Commands"
	@echo ""
	@echo "Setup:"
	@echo "  make setup          Install dev dependencies and pre-commit hooks"
	@echo "  make pre-commit     Run pre-commit checks manually"
	@echo ""
	@echo "Build:"
	@echo "  make build          Build all crates (debug)"
	@echo "  make build-release  Build all crates (release)"
	@echo "  make install        Install weavster CLI to ~/.cargo/bin"
	@echo ""
	@echo "Test & Lint:"
	@echo "  make test           Run all tests"
	@echo "  make lint           Run clippy with warnings as errors"
	@echo "  make fmt            Format code with rustfmt"
	@echo "  make fmt-check      Check formatting without modifying"
	@echo "  make check          Run fmt-check, lint, and test"
	@echo ""
	@echo "Run:"
	@echo "  make run            Run CLI (debug) - pass ARGS='--help'"
	@echo "  make dev            Watch for changes and rebuild"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean          Remove build artifacts"
	@echo "  make clean-all      Remove build artifacts and cached data"
	@echo ""
	@echo "Examples:"
	@echo "  make run ARGS='--help'"
	@echo "  make run ARGS='init my-project'"
	@echo "  make test ARGS='--package weavster-core'"

# ============================================================================
# Setup
# ============================================================================

setup:
	@echo "Checking Rust toolchain..."
	rustup show
	@echo ""
	@echo "Installing Rust development tools..."
	rustup component add rustfmt clippy
	@echo ""
	@echo "Installing cargo-watch for development..."
	cargo install cargo-watch || true
	@echo ""
	@echo "Installing prek (fast pre-commit in Rust)..."
	@if command -v prek >/dev/null 2>&1; then \
		echo "prek already installed"; \
	elif command -v brew >/dev/null 2>&1; then \
		brew install j178/tap/prek; \
	elif command -v cargo >/dev/null 2>&1; then \
		cargo install prek; \
	else \
		echo "ERROR: Could not install prek. Install manually:"; \
		echo "  brew install j178/tap/prek"; \
		echo "  OR"; \
		echo "  cargo install prek"; \
		exit 1; \
	fi
	@echo ""
	@echo "Installing git hooks..."
	prek install
	prek install --hook-type pre-push
	@echo ""
	@echo "Setup complete! Git hooks are now active."

# Run prek manually on all files
pre-commit:
	prek run --all-files

# ============================================================================
# Build
# ============================================================================

build:
	cargo build

build-release:
	cargo build --release

install:
	cargo install --path crates/weavster-cli

# ============================================================================
# Test & Lint
# ============================================================================

test:
	cargo test $(ARGS)

lint:
	cargo clippy -- -D warnings

fmt:
	cargo fmt

fmt-check:
	cargo fmt --check

# Run all checks (what CI would run)
check: fmt-check lint test
	@echo ""
	@echo "All checks passed!"

# ============================================================================
# Run
# ============================================================================

# Run the CLI with optional arguments
# Usage: make run ARGS='--help'
run:
	cargo run -p weavster-cli -- $(ARGS)

# Run release build
run-release:
	cargo run --release -p weavster-cli -- $(ARGS)

# Watch for changes and rebuild (requires cargo-watch)
dev:
	cargo watch -x 'build -p weavster-cli'

# Watch and run tests on change
dev-test:
	cargo watch -x test

# ============================================================================
# Cleanup
# ============================================================================

clean:
	cargo clean

clean-all: clean
	rm -rf .weavster/

# ============================================================================
# Documentation
# ============================================================================

docs:
	cargo doc --no-deps --open

docs-build:
	cargo doc --no-deps
