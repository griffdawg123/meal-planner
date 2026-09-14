# Agent Instructions

## Repository remotes and credential boundary

GitHub credentials must remain on the user's local computer. Never add GitHub tokens, SSH keys, credential helpers, or `gh` authentication to an Amp orb or to repository files.

Determine the execution location before performing remote operations:

- Local checkout: `origin` points to GitHub and `orb` points to the Amp-hosted mirror.
- Amp orb checkout: `origin` points to the Amp-hosted repository. GitHub is intentionally unavailable.
- If the topology is different or ambiguous, do not push; explain what was found and ask the user.

Do not rename, replace, or add remotes outside `dev/orb setup`. Never force-push.

## Local-to-orb development workflow

Use the versioned `dev/orb` command and follow `docs/orb-workflow.md`.

- Start remote development locally with `dev/orb start '<prompt>'`.
- In an orb, implement and verify the requested change, but do not access GitHub or push a Git remote. Leave the working tree available for synchronization.
- Bring orb changes to the originating local worktree with `dev/orb sync T-...`.
- Review and publish from the local checkout with `dev/orb publish -m '<message>'`.
- Use one Git worktree per concurrent orb task.

`amp sync` is one-way: it mirrors an orb's live working-tree changes to a local checkout. It does not upload local edits to the orb, and it is not equivalent to `git pull`.

## Sending development tasks to an orb

A local agent may delegate a well-scoped development task to a new orb when the user requests remote execution or when isolated, asynchronous execution is useful. Keep small local edits and tasks that depend on uncommitted local state in the current checkout.

Before delegation:

1. Confirm the local working tree is clean and `HEAD` exactly matches `origin/main`. Never send uncommitted files implicitly.
2. Use a dedicated Git worktree when another task is active or the work may run concurrently.
3. Write a complete task prompt containing the outcome, relevant context, scope, constraints, and required verification. The orb does not inherit the local conversation or uncommitted state.
4. Start the task with `dev/orb start '<prompt>'`. Do not call raw `amp -ox` unless the workflow script cannot support a required option.
5. Preserve the returned thread URL. Use that thread for follow-up rather than starting a replacement task.

The local agent remains responsible for the outcome. After the orb finishes, run `dev/orb sync T-...`, inspect every changed file, run the appropriate combined checks, and resolve issues locally or send an explicit task branch back to the same orb. Do not publish merely because the orb reports success.

For independent concurrent tasks, create one branch and worktree per task before calling `dev/orb start`. Never sync two orb threads into the same dirty worktree.

## Returning local edits to an existing orb

When the user wants local follow-up edits sent back to an existing orb, use an explicit Amp-hosted task branch:

1. On the local computer, commit the edits and push the task branch to the `orb` remote.
2. In the orb, first inspect the working tree and current branch. Do not overwrite uncommitted or divergent work.
3. Fetch the task branch from the orb's `origin` remote.
4. Integrate it with a fast-forward-only merge when possible, then continue work and verification.

Typical commands are:

```bash
# Local checkout
git push orb HEAD:task/<name>

# Amp orb checkout
git fetch origin task/<name>
git merge --ff-only origin/task/<name>
```

If fast-forward integration is impossible, stop and report the divergence rather than rebasing, resetting, force-pushing, or guessing which side should win.

## Publishing

Only a local checkout may write to GitHub. Publishing requires the user's explicit request and must run the configured tests after rebasing onto the latest GitHub base branch. The Amp-hosted mirror may be advanced only after GitHub publication succeeds.
