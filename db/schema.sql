PRAGMA foreign_keys = ON;

BEGIN;

CREATE TABLE household (
    id                   TEXT PRIMARY KEY
                               CHECK (id <> ''),
    name                 TEXT NOT NULL
                               CHECK (name = trim(name) AND name <> ''),
    timezone             TEXT NOT NULL
                               CHECK (timezone = trim(timezone) AND timezone <> ''),
    created_by_member_id TEXT NOT NULL
                               CHECK (created_by_member_id <> ''),
    created_at           TEXT NOT NULL
                               DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

    FOREIGN KEY (id, created_by_member_id)
        REFERENCES member (household_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE member (
    id           TEXT PRIMARY KEY
                      CHECK (id <> ''),
    household_id TEXT NOT NULL
                      CHECK (household_id <> ''),
    name         TEXT NOT NULL
                      CHECK (name = trim(name) AND name <> ''),
    created_at   TEXT NOT NULL
                      DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

    UNIQUE (household_id, id),
    FOREIGN KEY (household_id)
        REFERENCES household (id)
        ON DELETE CASCADE
);

-- Future household-scoped tables should reference household(id).
-- Future member preferences and auth identities should reference member(id).

COMMIT;
