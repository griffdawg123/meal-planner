# Household and member schema

[`schema.sql`](schema.sql) defines the two foundational entities for the MVP. IDs are
application-supplied `TEXT` values so callers can generate stable UUIDs without relying on a
SQLite-specific ID extension. Both tables record creation time as an ISO 8601 UTC text value,
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

SQLite foreign-key enforcement is connection-local. The schema enables it while applying the DDL;
every application connection must also execute `PRAGMA foreign_keys = ON`.
