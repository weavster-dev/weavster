# GitHub Workflows - Technical Documentation

> **For Contributors:** See [CONTRIBUTING.md](../../CONTRIBUTING.md) for contribution guidelines.
> **For Maintainers:** This document covers workflow internals and troubleshooting.

## Workflow Architecture

### Core Workflows

| Workflow | Trigger | Purpose | Outputs |
|----------|---------|---------|---------|
| `main.yml` | Push/PR to `main`/`develop` | CI/CD checks | Test results, coverage |
| `develop-publish.yml` | Push to `develop` | Dev builds | TestPyPI package, dev release |
| `release-candidate.yml` | Push to `release/**` | RC builds | TestPyPI RC, pre-release |
| `production-release.yml` | Merge to `main` | Production | PyPI package, stable release |
| `docs-deploy.yml` | Docs changes on `main` | Documentation | GitHub Pages |
| `on-release-main.yml` | Manual releases | Legacy fallback | PyPI package |

## Secrets Configuration

| Secret | Purpose | Scope | Notes |
|--------|---------|-------|-------|
| `PYPI_TOKEN` | Production releases | Entire account → Project specific | Update scope after first release |
| `TEST_PYPI_TOKEN` | Dev/RC builds | Entire account | For TestPyPI only |
| `CODECOV_TOKEN` | Coverage reports | Repository | Optional but recommended |

## Version Management

**Dynamic versioning via hatch-vcs:**
- `develop`: `0.1.0.dev123+g1234567.d20240101`
- `release/*`: `0.1.0rc1`, `0.1.0rc2` (auto-incremented)
- `main`: `0.1.0` (clean production)

**Configuration:** `pyproject.toml` → `[tool.hatch.version.raw-options]`

## Troubleshooting

### Common Issues

**❌ "Package already exists" on PyPI**
```bash
# Check if version already published
pip index versions weavster
# Increment version or use different build number
```

**❌ Workflow permissions denied**
- Check branch protection rules
- Verify `GITHUB_TOKEN` has write permissions
- Ensure required status checks are configured

**❌ TestPyPI publish fails**
```bash
# Verify token scope and permissions
# Check package name conflicts on TestPyPI
# Ensure package builds successfully locally
uv build && ls dist/
```

**❌ Documentation deployment fails**
- Verify GitHub Pages is enabled (Settings → Pages)
- Check `mkdocs.yml` configuration
- Ensure all docs dependencies in `pyproject.toml`

**❌ RC auto-increment fails**
```bash
# Check git tags manually
git tag -l "v*rc*" | sort -V
# Verify tag format matches expected pattern
```

### Debug Workflow Runs

1. **Check Actions tab** for detailed logs
2. **Review failed step outputs**
3. **Verify secrets are set** (Settings → Secrets)
4. **Test locally** before pushing:
   ```bash
   make test && make check && uv build
   ```

### Workflow Dependencies

**Required files:**
- `.github/actions/setup-python-env/action.yml`
- `pyproject.toml` (with proper hatch-vcs config)
- `Makefile` (with `test`, `check`, `build` targets)

**External dependencies:**
- PyPI/TestPyPI accounts with API tokens
- Codecov account (optional)
- GitHub Pages enabled

## Maintenance

### Regular Tasks
- **Monthly:** Update GitHub Actions to latest versions
- **Per Release:** Verify all workflow steps complete successfully
- **Quarterly:** Review and rotate API tokens

### Security
- **Token rotation:** Update PyPI tokens annually
- **Permissions review:** Audit workflow permissions quarterly
- **Dependency updates:** Keep actions/@v4 current

## Advanced Configuration

### Custom Environments
- **Production:** Requires manual approval for releases
- **GitHub Pages:** Automatic deployment from `gh-pages` branch

### Notification Setup
Add Slack/Discord webhooks to workflow files for alerts on:
- Failed builds
- Successful releases
- Security issues

---

**🔧 For workflow modifications, test thoroughly in a fork before merging to main.**
