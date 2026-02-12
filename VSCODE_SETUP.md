# VSCode Setup Complete ✨

Your VSCode environment is now optimized for Rust development with Claude Code!

## What Was Configured

### 1. Extensions ([.vscode/extensions.json](/.vscode/extensions.json))

**Essential:**
- `rust-lang.rust-analyzer` - Rust language server
- `vadimcn.vscode-lldb` - Debugger for Rust

**Code Quality:**
- `tamasfe.even-better-toml` - TOML support
- `redhat.vscode-yaml` - YAML validation
- `esbenp.prettier-vscode` - Auto-formatting
- `usernamehw.errorlens` - Inline error display
- `serayuzgur.crates` - Dependency management

**Utilities:**
- `editorconfig.editorconfig` - Editor consistency
- `eamodio.gitlens` - Git enhancements
- `streetsidesoftware.code-spell-checker` - Spell checking
- `yzhang.markdown-all-in-one` - Markdown support

### 2. Workspace Settings ([.vscode/settings.json](/.vscode/settings.json))

**Rust-Analyzer:**
- Runs `cargo clippy -- -D warnings` on check
- Enables all workspace features
- Shows inlay hints for types and parameters
- Displays code lens for references and implementations

**Auto-Formatting:**
- `*.rs` → `cargo fmt`
- `*.toml` → `taplo`
- `*.yaml`, `*.yml` → `prettier`
- `*.md`, `*.json` → `prettier`

**Editor:**
- 100-character ruler for Rust
- Bracket colorization enabled
- Trim trailing whitespace
- Insert final newline

**File Nesting:**
- Groups related files in explorer
- `Cargo.toml` shows `Cargo.lock`, etc.

### 3. Debug Configurations ([.vscode/launch.json](/.vscode/launch.json))

- Debug unit tests in `weavster-core`
- Debug CLI executable
- Debug `weavster run --once`
- Debug `weavster init`

### 4. Tasks ([.vscode/tasks.json](/.vscode/tasks.json))

- `cargo build`
- `cargo test` (default test task)
- `cargo clippy`
- `cargo fmt`
- `cargo check`
- **Pre-commit checks** (runs fmt, clippy, test sequentially)

### 5. Claude Code Hooks ([.claude/settings.json](/.claude/settings.json))

**PostToolUse Hooks:**
- Auto-format Rust files with `cargo fmt`
- Auto-format TOML files with `taplo`
- Auto-format YAML files with `prettier`
- Auto-format Markdown/JSON files with `prettier`

**Enabled Plugins:**
- `rust-analyzer-lsp` - LSP integration
- `ralph-loop` - Iterative workflow

### 6. Additional Configuration

- [.prettierrc.json](/.prettierrc.json) - Prettier settings
- [.editorconfig](/.editorconfig) - Editor consistency across IDEs
- [.vscode/README.md](/.vscode/README.md) - Detailed setup guide
- [.gitignore](/.gitignore) - Updated to allow VSCode configs

## Quick Start

### 1. Install Extensions

```
Cmd+Shift+P → "Extensions: Show Recommended Extensions" → Install All
```

### 2. Install Required Tools

```bash
# Update Rust
rustup update
rustup component add rust-analyzer rustfmt clippy

# Install TOML formatter
cargo install taplo-cli --locked

# Install Prettier (requires Node.js)
npm install -g prettier
```

### 3. Verify Setup

```bash
cargo build
cargo test
cargo clippy -- -D warnings
```

## Key Features

### Format on Save
All files auto-format when you save (Cmd+S / Ctrl+S)

### Inline Errors
Error Lens shows Clippy warnings and errors directly in your code

### Code Actions
Press `Cmd+.` / `Ctrl+.` on any code to see available quick fixes

### Debug with Breakpoints
1. Click gutter to set breakpoint
2. Press `F5` to start debugging
3. Use debug toolbar to step through

### Run Tasks
Press `Cmd+Shift+B` / `Ctrl+Shift+B` to run the default build task

### Smart Code Completion
rust-analyzer provides:
- Auto-complete
- Go to definition (F12)
- Find references (Shift+F12)
- Inline type hints
- Documentation on hover

## Claude Code Integration

When Claude Code edits files, they're automatically formatted via hooks:

```
Edit .rs file → cargo fmt runs automatically
Edit .toml file → taplo fmt runs automatically
Edit .yaml file → prettier runs automatically
```

This ensures consistent formatting across all Claude Code changes!

## Keyboard Shortcuts

| Action | macOS | Windows/Linux |
|--------|-------|---------------|
| Quick Open File | Cmd+P | Ctrl+P |
| Command Palette | Cmd+Shift+P | Ctrl+Shift+P |
| Go to Definition | F12 | F12 |
| Find References | Shift+F12 | Shift+F12 |
| Quick Fix | Cmd+. | Ctrl+. |
| Start Debug | F5 | F5 |
| Build Task | Cmd+Shift+B | Ctrl+Shift+B |
| Toggle Terminal | Cmd+J | Ctrl+J |

## Troubleshooting

### Extensions Not Installing?
Open Command Palette → "Extensions: Show Recommended Extensions"

### Format on Save Not Working?
1. Check formatters are installed: `cargo fmt --version`, `taplo --version`, `prettier --version`
2. Reload window: Cmd+Shift+P → "Developer: Reload Window"

### rust-analyzer Issues?
1. Check output: View → Output → Select "rust-analyzer"
2. Reinstall: `rustup component add rust-analyzer`
3. Reload VSCode

## Next Steps

✅ Install recommended extensions
✅ Install required tools (taplo, prettier)
✅ Run `cargo build && cargo test`
✅ Start coding with auto-formatting and inline errors!

---

📖 For more details, see [.vscode/README.md](.vscode/README.md)
