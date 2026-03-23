# Workflow Changelog

## 2026-03-23 - Performance Optimization

### ⚡ Optimized String Allocations in Code Generation
- Replaced `push_str(&format!(...))` with `writeln!(...)` or `write!(...)` in `crates/weavster-codegen/src/generator.rs`.
- This reduces intermediate string allocations during Rust source code generation, improving overall performance.
- Fixed Clippy `write_with_newline` warnings by using `writeln!` where appropriate.
- Ensured consistent code formatting with `cargo fmt`.
