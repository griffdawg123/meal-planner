# Automated Dev Loop (`dev/devloop`)

This automates the cycle: pick a `task` issue off the backlog, implement it
with a failing-test-first change, open a PR, get an independent AI review,
and merge once tests and review both pass — then move to the next issue.
Only `dev/devloop` itself touches GitHub (`gh`); it follows the same
credential boundary as [`dev/orb`](orb-workflow.md).

## One-time setup

```bash
dev/orb configure-test 'go test ./...'   # reused as the loop's test gate
dev/devloop init-labels                  # creates devloop:in-progress / devloop:needs-attention
dev/devloop doctor                       # sanity-checks all of the above
```

By default both roles use the `claude` backend (`dev/backends/claude`), but
as two independent invocations: implementation via headless `claude -p`,
review via `claude ultrareview` (a separate cloud-hosted multi-agent review,
not the same agent grading its own work). Override either with local git
config if you want a different pairing:

```bash
git config --local devloop.implementerBackend claude
git config --local devloop.reviewerBackend claude
```

`dev/devloop` refuses to run if both resolve to the same backend — the
review has to be independent of the implementation.

## Running it

```bash
dev/devloop run           # loop continuously until idle, paused, or a failure
dev/devloop run --once    # do a single issue and stop
```

## What one cycle does

1. Picks the lowest-numbered open `task` issue that isn't already labeled
   `devloop:in-progress` or `devloop:needs-attention`. If the issue body
   references `Parent story: #N`, fetches that story's body too (acceptance
   criteria usually live there, not on the task).
2. Labels the issue `devloop:in-progress`, creates
   `../meal-planner-issue-<n>` as a fresh worktree/branch off the base
   branch (mirrors the concurrent-orb pattern in `docs/orb-workflow.md`).
3. Writes a prompt file combining the issue, the parent story, and this
   repo's house rules (failing-test-first, Go test conventions, don't touch
   git/GitHub), then dispatches it to the implementer backend.
4. Runs `gofmt -l`, `go vet ./...`, and the configured test command in the
   worktree. Any failure here — or a non-zero implementer exit — labels the
   issue `devloop:needs-attention`, leaves the worktree for inspection, and
   **stops the loop** (no unattended retries).
5. Commits, pushes the branch, opens the PR with `Closes #<n>`.
6. Dispatches the reviewer backend against the PR. A non-zero exit (findings
   reported) does the same needs-attention-and-stop as step 4.
7. If every PR check is green too (`gh pr checks --watch`, not `--required` —
   `main` isn't branch-protected yet, and `--required` would silently see
   zero required checks and pass trivially), merges (`--squash
   --delete-branch`), removes the worktree, and moves to the next issue.

## Usage-limit pausing

If a `claude -p` implement or `claude ultrareview` review call hits the
daily usage cap, the backend writes `$(git rev-parse --git-path
devloop-state)/paused` instead of failing the task, and exits 75. The
orchestrator leaves the issue's label, the worktree, and its own resume
state exactly as they are and exits (also 75) rather than escalating to
`devloop:needs-attention`. Just run `dev/devloop run` again later — it
picks the same in-flight issue back up. Nothing currently clears the
`paused` marker automatically; delete
`$(git rev-parse --git-path devloop-state)/paused` once you know the cap
has reset, or wire `dev/devloop run` into a `/loop` or cron wrapper that
polls on an interval and treats exit 75 as "try again later" rather than an
error.

The usage-limit detection is a text heuristic (`dev/backends/claude`'s
`USAGE_LIMIT_PATTERN`) that hasn't been confirmed against a real occurrence
yet — tighten it the first time a run pauses unexpectedly or fails to pause
when it should have.

## Self-improving AGENTS.md

The implementer prompt asks the agent to call out, in its final summary,
any repo convention it had to discover the hard way rather than edit
`AGENTS.md` itself. Folding a small `AGENTS.md` addition into the same PR
(so it goes through the same review + merge gate as the code change) is a
deliberate manual step for now — not yet automated — because `AGENTS.md` is
what encodes this loop's own safety rules (including the auto-merge
carve-out below), so changes to it should never take a shortcut around
review.

## Backend contract

A backend is any executable at `dev/backends/<name>` implementing:

- `implement <worktree-path> <prompt-file>` — mutate the worktree in place
  into a complete, tested change. Exit 0 on success, 1 on failure, 75 if
  paused on a usage limit (after writing `$DEVLOOP_STATE_DIR/paused`).
  Must behave synchronously from the orchestrator's point of view — an
  async backend (e.g. an Amp orb) is responsible for polling/blocking
  internally until its remote work finishes before returning.
- `review <pr-number> <issue-number>` — read the PR diff and the issue
  (`gh`), post findings to the PR, and signal verdict via exit code (0 =
  pass, 1 = changes requested, 75 = paused). Must not modify the worktree.

`dev/devloop` sets `DEVLOOP_STATE_DIR` in the environment before invoking
either subcommand.

### Backends not yet built

- `dev/backends/amp` — wraps the existing `dev/orb start` / `sync`. Needs a
  short investigation into how to detect an orb thread's completion before
  writing this (`dev/orb start` returns as soon as the orb *starts*, not
  when it finishes).
- `dev/backends/openrouter-hermes` — same contract, for a future
  OpenRouter-hosted Hermes agent.

## Human role

Issue-raising and UAT are the only manual steps by design. UAT happens
**after** merge, on `main` — the loop does not wait for it. If you spot a
regression, raise a new issue; `dev/devloop` will pick it up like any other.
You can still steer the project directly at any point (edit an issue,
relabel it `devloop:needs-attention` to pull it out of the loop, or just
push a commit yourself) — the automation doesn't require staying hands-off.
