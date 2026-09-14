# Local GitHub and Amp Orb Workflow

This workflow delegates development to Amp orbs while keeping GitHub credentials and final publishing on the developer's computer.

## Security boundary

The two remotes have distinct responsibilities:

```text
origin  -> GitHub repository (authoritative; accessed locally)
orb     -> Amp-hosted mirror (development input for orbs)
```

The orb receives a committed snapshot through the Amp-hosted mirror. Finished files return with `amp sync`; the orb is instructed not to push. Only the local `publish` command writes to GitHub.

To make this a security boundary rather than merely a workflow convention, do not add GitHub credentials to the Amp project and disconnect Amp's GitHub integration. An Amp GitHub integration can issue short-lived credentials to an orb even though no token is stored there.

## Initial local setup

Install and sign in to the Amp CLI, then clone the Amp-hosted repository using the exact command shown on the Amp project page.

Create an empty GitHub repository from your local computer. Do not initialize it with a README because this repository already has history. Then configure the remotes:

```bash
dev/orb setup git@github.com:YOUR-USER/meal-planner.git
git push -u origin main
dev/orb doctor
```

`setup` renames the cloned Amp `origin` remote to `orb`, adds GitHub as the new `origin`, and records local-only workflow configuration in `.git/config`.

Once the application has a test suite, configure its command locally:

```bash
dev/orb configure-test 'pnpm test'
```

## Delegate a task

Begin with a clean checkout whose `HEAD` exactly matches `origin/main`:

```bash
dev/orb start 'Implement recipe URL importing and test it'
```

The command:

1. Fetches GitHub and refuses to continue if the local baseline is stale or unpublished.
2. Pushes that exact commit to the Amp-hosted mirror without force.
3. Starts a new orb and tells it not to access GitHub or push a remote.
4. Records the thread and baseline under the worktree's Git metadata.

It prints the Amp thread URL immediately. The orb continues working after the command exits.

## Bring the work back

After the orb finishes:

```bash
dev/orb sync T-...
```

The thread argument is optional for the most recently started task. Sync refuses to apply changes if the checkout is dirty, the checkout has moved from the delegated baseline, or the thread belongs to a different worktree.

Review the resulting diff and run any additional checks before publishing.

## Publish locally

Publish uncommitted synced changes with:

```bash
dev/orb publish -m 'Implement recipe URL importing'
```

`publish` commits the reviewed files, fetches and rebases onto `origin/main`, runs Git integrity checks and the configured test command, then pushes from the local computer. On `main`, it also advances the Amp mirror to the same commit. On a feature branch, it pushes that branch to GitHub and leaves the mirror unchanged until the work reaches `main`.

## Concurrent tasks

Use a separate Git worktree and branch for each concurrent orb. Every worktree keeps independent handoff metadata and prevents one task's synced files from colliding with another's:

```bash
git worktree add ../meal-planner-recipe-import -b recipe-import main
cd ../meal-planner-recipe-import
dev/orb start 'Implement recipe URL importing and test it'
```

Each task must begin at the current `origin/main` commit. Sync from the same worktree that started the task.

## Failure behavior

- No operation force-pushes.
- Dirty or stale handoffs are rejected.
- Divergence in the Amp mirror causes the mirror push to fail for manual investigation.
- Rebase conflicts stop `publish` before tests or pushes.
- Failed tests stop publication.
- When no test command is configured, `publish` says so explicitly and runs only Git whitespace/integrity checks.
