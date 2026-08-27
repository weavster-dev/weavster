# Documentation Site

This page describes **how** the documentation site is built, served, and deployed.
For **what** goes into it (user-facing examples, CLI docs, API refs), see
[AGENTS.md §Documentation](../AGENTS.md#documentation).

## Stack

| Layer | Choice |
|---|---|
| Generator | [MkDocs](https://www.mkdocs.org/) `1.6.1` |
| Theme | mkdocs default (readthedocs) |
| Hosting | GitHub Pages (`gh-pages` branch) |
| Deploy trigger | Push to `main` (GitHub Actions) |
| Python | `3.12` (CI), any `>=3.10` locally |

## Local development

```bash
# Install (one-time, or after pulling a requirements.txt change)
pip install -r requirements.txt

# Preview with live reload
mkdocs serve

# Open http://localhost:8000
```

Changes to `docs/` files appear immediately in the browser. The MkDocs
config is at [`mkdocs.yml`](../mkdocs.yml) (repo root).

## Navigation

The site nav is defined in `mkdocs.yml` under the `nav:` key. Each entry
maps a display title to a file under `docs/`. Current pages:

- **Home** — `docs/index.md`
- **User Guide** — `docs/guide/` (CLI, config, API, adapters, etc.)
- *(expand as features ship)*

To add a page, create the `.md` file and add a `nav:` entry.

## Deploy

The docs deploy automatically on every push to `main` via
[`.github/workflows/docs.yml`](../.github/workflows/docs.yml):

```bash
# Manual trigger (equivalent to what CI does):
mkdocs gh-deploy --force
```

This builds the site into `site/` and pushes it to the `gh-pages` branch.
GitHub Pages serves it at `https://weavster-dev.github.io/weavster/`.

## Versioning

Not yet versioned. The `gh-pages` branch always reflects `main`. When
versioned docs are added, the plan is to use
[mike](https://github.com/jimporter/mike) with a `versions.json` file.

## Excluded files

Files under `docs/` that are **not** part of the public site are listed
in `mkdocs.yml` under `exclude_docs:` — currently `prompt-3-kickoff.md`.
These are still tracked in the repo but omitted from the built site.