# Application schema

[`schema.sql`](schema.sql) defines the foundational entities for the MVP. IDs are
application-supplied `TEXT` values so callers can generate stable UUIDs without relying on a
SQLite-specific ID extension. Household-domain tables record creation time as an ISO 8601 UTC
text value, which sorts chronologically and is readable in SQLite. Names must be non-empty and trimmed.
`household.timezone` is also required and trimmed; the service must validate it against the IANA
timezone database because SQLite does not include one. Updated timestamps are omitted until a
concrete audit or synchronization requirement justifies maintaining them.

Each member belongs to exactly one household. `ON DELETE CASCADE` deliberately removes those
members when their household is deleted, avoiding orphaned identity join points during future
account/data deletion. Future household-owned data should likewise carry a `household_id` foreign
key, while individual preferences and identities reference `member.id`.

`household.created_by_member_id` records the first member explicitly. Its composite foreign key to
`member(household_id, id)` guarantees that the recorded creator is a member of that same household,
not merely an existing member elsewhere. The key is deferred because creation is intentionally one
transaction: insert the household with the new member ID, insert that member with the household ID,
then commit. A commit without the member fails. The deferred `NO ACTION` also prevents deleting the
creator member by itself, while still allowing deletion of the household and its cascading members
in one operation.

The `preference` table stores household and individual preferences in one model:

- `id` is the application-supplied preference identifier, and `created_at` is its creation time.
- `household_id` identifies the owning household and is always required. `member_id` is `NULL` for
  a household-level preference or contains the member ID for an individual preference.
- `kind` independently records whether the row is a `hard` constraint or `soft` preference.
- `category` classifies the target as an `allergy`, `dietary_restriction`, `dislike`, `cuisine`,
  `budget`, or `cooking_time`. Category and kind are independent, so any category can be hard or
  soft.
- `value` is the non-empty, trimmed target, such as `peanuts`, `Italian`, or `under 30 minutes`.
  A single text field accommodates the categories' deliberately different vocabularies and units
  without nullable category-specific columns; applications can adopt canonical values within each
  category when resolution logic is implemented.
- `strength` is required for soft rows and ranges from 1 (weak) through 5 (strong), giving future
  resolution logic a weight for balancing members. It must be `NULL` for hard constraints because
  those are mandatory rather than weighted.

The composite foreign key from `preference(household_id, member_id)` to
`member(household_id, id)` makes it impossible to assign an individual preference to a member of a
different household. SQLite permits the composite reference when `member_id` is `NULL`, which is
the intentional household-level scope. Both foreign keys cascade deletion so preferences cannot
outlive their household or individual owner.

The `away_night` table records attendance as exceptions: each row says one member will not be
eating dinner at home on one night. Members are attending by default, so a night with no row for a
member means that member is present, and the MVP needs no row per member per night. This matches
how members state attendance ("I'm out on Tuesday") and keeps the default of portions matching
attending members cheap to compute.

- `night` is the calendar date of the dinner, formatted `YYYY-MM-DD`, in the household's
  timezone. It is a local date rather than a UTC instant because a dinner belongs to a household
  night regardless of when it is recorded. The `night IS date(night)` check rejects other formats,
  times, and impossible dates such as `2026-02-30`.
- `(member_id, night)` is the primary key, so a member is either away or not on a given night and
  recording the same night twice is rejected. Changing attendance after a draft exists is a plain
  insert (now away) or delete (now present again), with no status column to keep consistent.
- The composite foreign key to `member(household_id, id)` prevents recording an away night for a
  member of another household, and both foreign keys cascade so away nights cannot outlive their
  household or member.
- The `(household_id, night)` index serves the planner's lookup of who is away across the nights
  of a planning period.

The table only records attendance. How planning weights an away member's soft preferences, and
the rule that hard constraints of present members still apply, belong to the planning logic
(`household.Service.NightPreferences`).
Recurring "regular nights out" are deferred; when added they can expand into or sit alongside
these dated rows.

Web authentication is stored in three tables. `web_identity` links one normalized email address
to a member. `magic_link` stores short-lived, single-use login challenges, and `web_session` stores
the resulting authenticated sessions. Both bearer-token tables persist only SHA-256 token hashes;
the raw token is returned to the caller for delivery or use and cannot be recovered from the
database. Their timestamps are Unix seconds so expiry checks are direct integer comparisons.

A magic link belongs to exactly one member, and the schema itself enforces its lifecycle rather
than leaving it to the service. `token_hash` must be a non-NULL 32-byte BLOB (SQLite allows NULL
in non-integer primary keys, and `length()` would accept a 32-character TEXT value). All three
timestamps must be stored as integers (SQLite does not enforce column types, and a TEXT
`expires_at` would compare greater than every integer `now`, creating a link that never expires).
`expires_at` must be after `created_at`. `used_at` is `NULL` until the link is consumed, and it may
only be set within the link's lifetime: `created_at <= used_at < expires_at`, which matches the
service's strict `expires_at > now` check.

The `magic_link_valid_insert` and `magic_link_valid_update` triggers enforce these rules, and the
`magic_link_single_use` trigger rejects any update to `used_at` once it is set, so a consumed link
cannot be reset or consumed again. The rules are triggers rather than `CHECK` constraints so that
reapplying the schema installs them on existing databases too; `CREATE TABLE IF NOT EXISTS` cannot
add constraints to a table that already exists. Triggers only guard new writes, so applying the
schema also deletes any existing row that breaks these rules. Such a row can only predate the
triggers, and discarding it is safe because a member can always request a new link. Expired or
used rows are inert and may be pruned at any time.

All three tables cascade on member deletion so removed members immediately lose their web
identities, unused links, and sessions.

Telegram identities are stored in two tables and linked with a one-time code:

1. A member who is already authenticated (for example through a web session) asks for a linking
   code. The service generates a short random code, stores its SHA-256 hash in
   `telegram_link_code` against that member, and shows the raw code to the member.
2. The member sends the code to the bot (for example `/link ABCD-1234`). In one transaction the
   service consumes the unexpired, unused code by setting `used_at` and inserts a
   `telegram_identity` row pairing the sender's Telegram user ID with the code's member.
3. For every later message, the bot looks up the sender's Telegram user ID in
   `telegram_identity` and joins `member` to obtain the household and member. A user ID with no
   row is unlinked and must not be allowed to read or change any household's data.

The code is issued to a known member rather than typed into the bot first, so possession of the
code proves the Telegram account belongs to that member and a Telegram user can never choose the
household or member they are attributed to.

`telegram_identity` is keyed by `telegram_user_id`, Telegram's stable numeric user ID (the
message's `from.id`), not a username or chat ID: usernames can change or be absent, and a chat ID
identifies a conversation (possibly a group) rather than the person who sent a message. The ID must
be a positive integer; the table is `WITHOUT ROWID` so a `NULL` ID is rejected instead of becoming an
automatically assigned rowid. The key makes each Telegram account resolve to exactly one member,
and `member_id` is `UNIQUE`, matching `web_identity`, so a member has at most one linked Telegram
account. Relinking a member to a different account is a delete followed by an insert. `linked_at`
records when the link was made, in Unix seconds.

`telegram_link_code` mirrors `magic_link`: only a 32-byte SHA-256 hash of the code is stored, the
code belongs to exactly one member, all timestamps are integer Unix seconds, `expires_at` is after
`created_at`, and `used_at` may only be set within `created_at <= used_at < expires_at`. The
`telegram_link_code_single_use` trigger rejects any change to `used_at` once it is set, so a code
links at most one Telegram account. Because these are new tables, their rules are `CHECK`
constraints; only the single-use rule, which compares old and new values, needs a trigger. Codes are
meant to be short enough to type, so the service must keep their lifetime brief (minutes) and limit
failed attempts; the hash keeps a leaked database from revealing live codes but cannot make a
low-entropy code resistant to offline guessing. Expired or used rows are inert and may be pruned at
any time. The `member_id` index serves pruning and cascading deletes.

`telegram_link_attempt` enforces that failed-attempt limit. It holds one row per Telegram user ID
with `failed_attempts`, the number of invalid codes sent since `window_started_at` (Unix seconds).
Each link attempt increments the count, or restarts it at 1 when the window has passed, in the same
transaction that checks the code. The service keeps that increment only when the code is invalid,
and once the count exceeds its limit it rejects every attempt, even one with a valid code, until
the window ends. A successful link deletes the row. Senders are usually not linked yet, so the table
has no foreign key; rows whose window has passed are inert and may be pruned at any time.

Both tables cascade on member deletion (and therefore household deletion), so a removed member's
Telegram account immediately becomes unlinked and their outstanding codes stop working.

SQLite foreign-key enforcement is connection-local. The schema enables it while applying the DDL;
every application connection must also execute `PRAGMA foreign_keys = ON`.
