# Documentation Site

This page describes **how** the documentation site is built, served, and deployed.
For **what** goes into it (user-facing examples, CLI docs, API refs), see
[AGENTS.md §Documentation](https://github.com/weavster-dev/weavster/blob/main/AGENTS.md#documentation).

## Stack

| Layer | Choice |
|---|---|
| Generator | [MkDocs](https://www.mkdocs.org/) `1.6.1` |
| Theme | MkDocs' built-in `mkdocs` theme (no `theme:` key) |
| Hosting | GitHub Pages (`gh-pages` branch), at <https://docs.weavster.dev/> (`docs/CNAME`) |
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
config is at [`mkdocs.yml`](https://github.com/weavster-dev/weavster/blob/main/mkdocs.yml) (repo root).

Build it the way CI does before you push:

```bash
mkdocs build --strict
```

Every build is strict (`strict: true` in `mkdocs.yml`): a broken link, a link to a heading that
does not exist, or a page left out of the nav fails it. Link to files outside `docs/` with their
full GitHub URL.

## Navigation

The site nav is defined in `mkdocs.yml` under the `nav:` key. Each entry
maps a display title to a file directly under `docs/` (there are no sub-folders of pages). To
add a page, create the `.md` file and add a `nav:` entry; a page missing from the nav fails the
build.

## Deploy

Every pull request builds the site (the `docs` job in
[`ci.yml`](https://github.com/weavster-dev/weavster/blob/main/.github/workflows/ci.yml)). The docs deploy automatically on every push to
`main` via [`docs.yml`](https://github.com/weavster-dev/weavster/blob/main/.github/workflows/docs.yml):

```bash
# Manual trigger (equivalent to what CI does):
mkdocs gh-deploy --force
```

This builds the site into `site/` and pushes it to the `gh-pages` branch.
GitHub Pages serves it at <https://docs.weavster.dev/>.

## Versioning

Not yet versioned. The `gh-pages` branch always reflects `main`. When
versioned docs are added, the plan is to use
[mike](https://github.com/jimporter/mike) with a `versions.json` file.

## Excluded files

Files under `docs/` that are **not** part of the public site are listed
in `mkdocs.yml` under `exclude_docs:` — currently the internal planning documents
`prompt-3-kickoff.md`, `mvp-project-plan.md`, and `agent-onboarding.md`.
These are still tracked in the repo but omitted from the built site.