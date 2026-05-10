# Enabling branch protection on `main`

GitHub does not let me set repository-level branch protection rules via
the API surface this repo's automation has access to. **You** (or any
admin) need to do this once, in the GitHub web UI. After the steps
below, every PR to `main` will require:

- All CI checks (`.github/workflows/ci.yml`) to pass.
- At least one approving review.
- The branch to be up-to-date with `main` before merging.

These are the exact settings the roadmap (Phase 1, task #2) calls for.

## Steps

1. **Settings → Branches → Add branch protection rule**.
2. **Branch name pattern**: `main`
3. Tick the following:
   - ☑ **Require a pull request before merging**
     - ☑ Require approvals: **1** (raise to 2 for sovereign deploys)
     - ☑ Dismiss stale pull request approvals when new commits are pushed
   - ☑ **Require status checks to pass before merging**
     - ☑ Require branches to be up to date before merging
     - In the search box, add each of these check names (all from
       `.github/workflows/ci.yml`):
       - `Go build + vet`
       - `Go integration tests`
       - `End-to-end smoke`
       - `Go vulnerability scan`
       - `Next.js apps`
   - ☑ **Require conversation resolution before merging**
   - ☑ **Require linear history** (optional, but it makes `git log`
     readable)
   - ☑ **Do not allow bypassing the above settings** (the strict mode)
4. **Create**.

## Local equivalent

Run this before opening a PR — it executes the same checks the
`Next.js apps` and `Go build + vet` jobs run, minus the integration /
smoke jobs which need a real Postgres:

```bash
make check
```

`make check` is intentionally fast (sub-2-minute warm) so it doesn't
discourage frequent runs. For the DB-dependent jobs, do:

```bash
make up         # postgres + redis + all services in docker
make migrate    # apply migrations against the local PG
make test       # go test ./... -race -count=1 across the workspace
make smoke      # end-to-end happy-path
```

## Why this isn't already on

The repo was created by automation that doesn't have admin scope on the
GitHub org. CI runs on every push, and PRs *can* see the check status —
but nothing prevents a force-merge or a merge with red checks until the
rules above are set.

## What changes when this lands

- `mcp__github__merge_pull_request` calls will block on red CI instead
  of merging through.
- Direct pushes to `main` (e.g. `git push origin main`) will be rejected.
  All work goes via PR from a feature branch.
- The `claude/*` branch convention this repo uses is unaffected — those
  are feature branches that target `main` via PR.
