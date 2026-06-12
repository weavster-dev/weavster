# Thin engine image (Engine Plan E5). Multi-stage: build with the Rust
# toolchain, then ship only the binary on a minimal glibc base — no Node, no TS
# toolchain (those ran at compile time, in the CLI). The engine *runs* compiled
# artifacts; it never sees the DSL.
#
# Build from the repo root (the build context is the Cargo workspace root):
#   docker build -f engine/Dockerfile -t weavster-engine .
#
# Run the golden pipeline (artifact built by `weavster compile examples/golden-path`):
#   docker run --rm -v "$PWD/examples/golden-path:/etc/weavster" weavster-engine
# The mounted weavster.yaml lands at /etc/weavster/weavster.yaml (the default
# config path); the artifact resolves by convention to
# /etc/weavster/target/artifact. Pass `--artifact <dir>` to override.

FROM rust:1.90-slim-bookworm AS build
WORKDIR /src
COPY Cargo.toml Cargo.lock ./
COPY engine ./engine
RUN cargo build --release -p weavster-engine

# distroless/cc carries glibc + libgcc + libstdc++ — all the engine binary's
# dynamic deps, and nothing else (no shell, no package manager).
FROM gcr.io/distroless/cc-debian12
COPY --from=build /src/target/release/weavster-engine /usr/local/bin/weavster-engine
# Drop privileges: distroless ships a `nonroot` user (uid 65532). The binary is
# world-readable/executable from COPY, so it runs fine; mounted volumes must be
# readable (and the sink dir writable) by this uid.
USER nonroot
ENTRYPOINT ["/usr/local/bin/weavster-engine"]
# Default invocation reads -c /etc/weavster/weavster.yaml.
