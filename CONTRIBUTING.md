# Contributing to Weavster

Thank you for your interest in contributing to Weavster! We welcome contributions from the community and are grateful for your help in making this project better.

## 🚀 Getting Started

Weavster uses a **Git Flow** workflow model. All contributions should be made through Pull Requests targeting the `develop` branch.

### Quick Start for Contributors

1. **Fork the repository** on GitHub
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/YOUR-USERNAME/weavster.git
   cd weavster
   ```
3. **Set up the development environment**:
   ```bash
   make install
   ```
4. **Create a feature branch** from `develop`:
   ```bash
   git checkout develop
   git pull origin develop
   git checkout -b feature/your-feature-name
   ```
5. **Make your changes** and commit them
6. **Push to your fork** and **create a Pull Request** targeting the `develop` branch

## 📋 Types of Contributions

### 🐛 Report Bugs

Report bugs at [GitHub Issues](https://github.com/weavster-dev/weavster/issues)

When reporting a bug, please include:
- Your operating system name and version
- Python version
- Weavster version
- Any details about your local setup that might be helpful in troubleshooting
- Detailed steps to reproduce the bug
- Expected behavior vs actual behavior

### 🔧 Fix Bugs

Look through the GitHub issues for bugs. Issues tagged with "bug" and "help wanted" are open to whoever wants to implement a fix.

### ✨ Implement Features

Look through the GitHub issues for features. Issues tagged with "enhancement" and "help wanted" are open to whoever wants to implement them.

### 📖 Write Documentation

Weavster could always use more documentation, whether as part of the official docs, in docstrings, or even on the web in blog posts, articles, etc.

### 💡 Submit Feedback

The best way to send feedback is to [file an issue](https://github.com/weavster-dev/weavster/issues).

If you are proposing a new feature:
- Explain in detail how it would work
- Keep the scope as narrow as possible to make it easier to implement
- Remember that this is a volunteer-driven project, and contributions are welcome! 🎉

## 🔄 Git Flow Workflow

This project follows the Git Flow branching model:

### Branch Structure
- **`main`**: Production-ready code with stable releases
- **`develop`**: Integration branch for features (target for PRs)
- **`release/vX.Y.Z`**: Release preparation branches
- **`feature/*`**: Feature development branches
- **`hotfix/*`**: Emergency fixes for production

### Contributing Process

1. **All Pull Requests should target the `develop` branch**
2. **Feature branches** should be created from `develop`:
   ```bash
   git checkout develop
   git pull origin develop
   git checkout -b feature/amazing-new-feature
   ```
3. **Hotfix branches** (for critical production fixes) should be created from `main`:
   ```bash
   git checkout main
   git pull origin main
   git checkout -b hotfix/critical-fix
   ```

## 🛠️ Development Setup

### Prerequisites
- Python 3.9 or higher
- [uv](https://docs.astral.sh/uv/) (recommended) or pip
- Git

### Setting Up Your Development Environment

1. **Clone the repository**:
   ```bash
   git clone https://github.com/weavster-dev/weavster.git
   cd weavster
   ```

2. **Install dependencies and set up pre-commit hooks**:
   ```bash
   make install
   ```

3. **Verify your setup**:
   ```bash
   make check
   make test
   ```

### Available Make Commands

- `make install` - Install dependencies and pre-commit hooks
- `make check` - Run all code quality checks
- `make test` - Run tests with coverage
- `make docs` - Build and serve documentation locally
- `make build` - Build the package
- `make clean-build` - Clean build artifacts

## ✅ Pull Request Process

### Before Submitting

1. **Run the full test suite**:
   ```bash
   make test
   ```

2. **Run code quality checks**:
   ```bash
   make check
   ```

3. **Update documentation** if needed:
   ```bash
   make docs
   ```

4. **Add tests** for new functionality

### Pull Request Guidelines

1. **Target the `develop` branch** (unless it's a hotfix)
2. **Use a clear and descriptive title**
3. **Fill out the PR template** completely
4. **Link to related issues** using keywords (e.g., "Fixes #123")
5. **Keep changes focused** - one feature/fix per PR
6. **Write clear commit messages** following conventional commits format:
   ```
   feat: add new CLI command for data validation
   fix: resolve memory leak in pipeline processing
   docs: update contribution guidelines
   ```

### PR Review Process

1. **Automated checks** must pass (CI/CD pipeline)
2. **Code review** by maintainers
3. **Testing** on development builds
4. **Approval and merge** into `develop`

## 📦 Package Testing

### Development Builds
After your PR is merged into `develop`, a development package will be automatically published to TestPyPI:

```bash
pip install --index-url https://test.pypi.org/simple/ weavster
```

### Release Candidates
When preparing for a release, release candidates are published:

```bash
pip install --index-url https://test.pypi.org/simple/ weavster==0.1.0rc1
```

## 🎨 Code Style

This project uses:
- **[Ruff](https://github.com/astral-sh/ruff)** for linting and formatting
- **[Pyright](https://github.com/microsoft/pyright)** for type checking
- **Line length**: 120 characters
- **Type hints** are required for public APIs
- **Docstrings** should follow Google style

Pre-commit hooks will automatically format your code, but you can also run:
```bash
uv run ruff format
uv run ruff check --fix
```

## 🧪 Testing

- Write tests for all new functionality
- Maintain or improve test coverage
- Use **pytest** for testing
- Tests are located in the `tests/` directory

Run tests locally:
```bash
make test
# or
uv run python -m pytest --cov
```

## 📚 Documentation

Documentation is built with **MkDocs** and automatically deployed to GitHub Pages.

To build docs locally:
```bash
make docs
# or
uv run mkdocs serve
```

## 🤝 Community Guidelines

- Be respectful and inclusive
- Follow the [Code of Conduct](CODE_OF_CONDUCT.md) (if applicable)
- Help others learn and grow
- Celebrate diversity and different perspectives

## 🏷️ Release Process (For Maintainers)

The project uses automated releases through Git Flow:

1. **Development** → `develop` branch → TestPyPI dev builds
2. **Release Preparation** → `release/vX.Y.Z` branch → TestPyPI RC builds
3. **Production** → `main` branch → PyPI stable releases

## 📞 Getting Help

- **Questions?** Open a [Discussion](https://github.com/weavster-dev/weavster/discussions)
- **Bugs?** Open an [Issue](https://github.com/weavster-dev/weavster/issues)
- **Features?** Start with a [Discussion](https://github.com/weavster-dev/weavster/discussions) first

## 🙏 Recognition

Contributors will be recognized in:
- The [Contributors](https://github.com/weavster-dev/weavster/graphs/contributors) page
- Release notes for their contributions
- The project's acknowledgments

Thank you for contributing to Weavster! 🎉

---

*This project is maintained by the Weavster team and the open-source community.*
