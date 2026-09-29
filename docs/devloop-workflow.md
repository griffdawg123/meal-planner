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
not the same agent grading its own work). A second backend,
`dev/backends/amp`, wraps the existing Amp orb workflow — set one role to
`amp` and the other to `claude` for real cross-provider independence rather
than two `claude` invocations:

```bash
git config --local devloop.implementerBackend amp
git config --local devloop.reviewerBackend claude
```

`amp implement` needs `amp login` run locally first (it wasn't logged in
when this was written, so that path is untested end-to-end — see the
caveats below before relying on it).

`dev/devloop` refuses to run if both resolve to the same backend — the
review has to be independent of the implementation.

## Running it

```bash
dev/devloop run           # loop continuously until idle, paused, or a failure
dev/devloop run --once    # do a single issue and stop
```

## What one cycle does

1. Picks the lowest-numbered open `task` issue that isn't already labeled
   `devloop:in-progress`, `devloop:needs-attention`, or
   `devloop:no-changes-needed`. If the issue body references
   `Parent story: #N`, fetches that story's body too (acceptance criteria
   usually live there, not on the task).
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
5. If the implementer backend succeeded but left the worktree with nothing
   to commit, the issue is already satisfied by existing code (commonly:
   another issue's work covered it too). Labels it
   `devloop:no-changes-needed`, comments why, and **closes it directly** —
   this doesn't stop the loop, it moves on to the next issue.
6. Otherwise commits, pushes the branch, opens the PR with `Closes #<n>`.
   Opening the PR is retried up to 3 times (15s, then 30s apart) because
   GitHub's API gives occasional transient 5xx errors. Each attempt first
   looks for an open PR for the branch, so a create that reported an error
   but actually went through is picked up rather than duplicated.
7. Dispatches the reviewer backend against the PR. If it requests changes,
   runs a **fix round** (see below) and reviews again, up to
   `devloop.maxFixRounds` times (default 2). Still requesting changes after
   that — or a reviewer that fails without leaving any findings — does the
   same needs-attention-and-stop as step 4.
8. If every PR check is green too (`gh pr checks --watch`, not `--required` —
   `main` isn't branch-protected yet, and `--required` would silently see
   zero required checks and pass trivially), merges (`--squash
   --delete-branch`), removes the worktree, and moves to the next issue.

## Fix rounds

When the reviewer requests changes, its findings go back to the implementer
instead of stopping the loop straight away:

1. The reviewer backend writes its findings to `$DEVLOOP_REVIEW_FEEDBACK_FILE`
   (kept at `$(git rev-parse --git-path
   devloop-state)/review-<issue>-round-<n>.md`).
2. `dev/devloop` builds a fix prompt: the original task prompt (issue, parent
   story, house rules) plus those findings quoted verbatim, and runs the
   implementer backend on the **same** worktree.
3. The same local checks as step 4 run again (gofmt, `go vet`, tests).
4. The change is committed as `Address review feedback on #<n> (round <k>)`
   and pushed to the existing PR branch — same PR, no new one.
5. The reviewer reviews the whole PR again.

It stops with `devloop:needs-attention` (and a notification) when:

- the reviewer still requests changes after `devloop.maxFixRounds` rounds,
- the implementer makes no changes in response to the feedback (usually it
  disagreed with a finding — its summary in the run output says why, and a
  human has to decide),
- any check fails in a fix round, exactly as for the first implementation.

The round number is part of the resume state, so a usage-limit pause in the
middle of a fix round resumes that round rather than starting over. Set
`git config --local devloop.maxFixRounds 0` to get the old behaviour back:
stop on the first changes-requested review.

The auto-merge gate is unchanged: a PR only merges once an independent
reviewer passes and CI is green. A fix round just gives the implementer a
chance to get there.

**amp as implementer can't do fix rounds yet.** `dev/orb start` requires
`HEAD` to exactly match `origin/main`, which stops being true once the
first commit is on the issue branch, so a fix round with the `amp`
implementer fails and stops with needs-attention. The `claude` implementer
is fine.

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

## Push notifications

Every stop condition — `devloop:needs-attention`, a pause, or the backlog
running dry — prints to stderr and, if configured, also sends a push
notification via [ntfy](https://ntfy.sh). This matters most for `dev/devloop
run` (continuous mode) or anything unattended: without it, the *only* signal
that the loop stopped is the GitHub label/comment, or noticing the process
died.

It's opt-in and off by default. Set your own topic yourself — a topic on the
public `ntfy.sh` instance is unauthenticated, so anyone who knows it can read
(or publish to) it, and there's no reason to tell anyone else, including an
agent, what it is:

```bash
git config --local devloop.ntfyTopic '<your-topic>'
git config --local devloop.ntfyServer 'https://ntfy.sh'   # optional; this is the default
```

`dev/devloop doctor` reports whether notifications are enabled (and warns if
`curl` isn't installed). A notification failure never affects the loop's own
exit code — `notify()` always swallows its own errors.

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
  On changes requested it must also write the findings to
  `$DEVLOOP_REVIEW_FEEDBACK_FILE` for the next fix round, specific enough to
  act on without extra context. Write that file **only** on a real
  changes-requested verdict, never when the backend itself fails: an exit 1
  with no feedback file is how the orchestrator tells a broken reviewer from
  one that asked for changes.

`dev/devloop` sets `DEVLOOP_STATE_DIR` in the environment before invoking
either subcommand, and `DEVLOOP_REVIEW_FEEDBACK_FILE` before `review`. A fix
round is just another `implement` call, on the existing worktree with a fix
prompt, so an implementer backend needs nothing extra to support it.

### The amp backend

`dev/backends/amp implement` dispatches via `dev/orb start` (from inside the
issue's worktree, where `dev/orb`'s own preconditions already hold — see
`docs/orb-workflow.md`), then has to detect when the orb is actually done
before it's safe to `dev/orb sync` — syncing mid-run would pull a partial,
still-being-written change. `dev/backends/amp review` sidesteps that problem
entirely: it doesn't use an orb at all, since a plain-text review needs no
repo checkout. It hands the PR diff and issue text to a synchronous, local
`amp -x` call and posts the result as a PR comment itself — which also
respects `AGENTS.md`'s rule that an orb must never hold GitHub credentials,
since this path never gives it any.

One thing in `implement` is now confirmed, one still isn't:

- **Completion detection** (`orb_thread_is_done`) polls `amp threads export
  <thread-id>` and checks `meta.lastKnownAgentState.state == "idle"`.
  Confirmed against a real, already-finished orb thread's export payload —
  but only the *finished* value; what it reports mid-task (still generating,
  running a tool) is still unconfirmed, since that requires catching a
  thread while it's actually running. It still fails closed: if it never
  reports idle within `DEVLOOP_AMP_TIMEOUT_SECONDS` (default 2700s),
  `implement` stops and tells you to run `dev/orb sync <thread-id>` yourself
  rather than guessing. Watch the first few real runs for it going idle too
  early (e.g. between tool calls rather than at the true end of the task),
  which would sync a partial change.
- **Headless permission prompts.** `amp -x` is documented as built for
  scripted/piped use, but whether it can hit an unanswerable interactive
  prompt during an orb task (and hang) isn't verified.

Confirm the remaining one against a real run before trusting `amp` as the
implementer backend fully unattended.

### Backends not yet built

- `dev/backends/openrouter-hermes` — same contract, for a future
  OpenRouter-hosted Hermes agent.

## Human role

Issue-raising and UAT are the only manual steps by design. UAT happens
**after** merge, on `main` — the loop does not wait for it. If you spot a
regression, raise a new issue; `dev/devloop` will pick it up like any other.
You can still steer the project directly at any point (edit an issue,
relabel it `devloop:needs-attention` to pull it out of the loop, or just
push a commit yourself) — the automation doesn't require staying hands-off.
