# Household, member, and preference schema

[`schema.sql`](schema.sql) defines the foundational entities for the MVP. IDs are
application-supplied `TEXT` values so callers can generate stable UUIDs without relying on a
SQLite-specific ID extension. All tables record creation time as an ISO 8601 UTC text value,
which sorts chronologically and is readable in SQLite. Names must be non-empty and trimmed.
`household.timezone` is also required and trimmed; the service must validate it against the IANA
timezone database because SQLite does not include one. Updated timestamps are omitted until a
concrete audit or synchronization requirement justifies maintaining them.

Each member belongs to exactly one household. `ON DELETE CASCADE` deliberately removes those
members when their household is deleted, avoiding orphaned identity join points during future
account/data deletion. Future household-owned data should likewise carry a `household_id` foreign
key, while individual preferences and Telegram or web identities can reference `member.id`.

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

SQLite foreign-key enforcement is connection-local. The schema enables it while applying the DDL;
every application connection must also execute `PRAGMA foreign_keys = ON`.
