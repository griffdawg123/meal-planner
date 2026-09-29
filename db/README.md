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
the rule that hard constraints of present members still apply, belong to the planning logic.
Recurring "regular nights out" are deferred; when added they can expand into or sit alongside
these dated rows.

Web authentication is stored in three tables. `web_identity` links one normalized email address
to a member. `magic_link` stores short-lived, single-use login challenges, and `web_session` stores
the resulting authenticated sessions. Both bearer-token tables persist only SHA-256 token hashes;
the raw token is returned to the caller for delivery or use and cannot be recovered from the
database. Their timestamps are Unix seconds so expiry checks are direct integer comparisons.

A magic link belongs to exactly one member, and the schema itself enforces its lifecycle rather
than leaving it to the service. `expires_at` must be after `created_at`. `used_at` is `NULL` until
the link is consumed, and it may only be set within the link's lifetime:
`created_at <= used_at < expires_at`, which matches the service's strict `expires_at > now` check.
The `magic_link_single_use` trigger rejects any update to `used_at` once it is set, so a consumed
link cannot be reset or consumed again. Expired or used rows are inert and may be pruned at any
time. All
three tables cascade on member deletion so removed members immediately lose their web identities,
unused links, and sessions.

SQLite foreign-key enforcement is connection-local. The schema enables it while applying the DDL;
every application connection must also execute `PRAGMA foreign_keys = ON`.
