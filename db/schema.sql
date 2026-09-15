PRAGMA foreign_keys = ON;

BEGIN;

CREATE TABLE IF NOT EXISTS household (
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

CREATE TABLE IF NOT EXISTS member (
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

CREATE TABLE IF NOT EXISTS preference (
    id           TEXT PRIMARY KEY
                      CHECK (id <> ''),
    household_id TEXT NOT NULL
                      CHECK (household_id <> ''),
    member_id    TEXT
                      CHECK (member_id IS NULL OR member_id <> ''),
    kind         TEXT NOT NULL
                      CHECK (kind IN ('hard', 'soft')),
    category     TEXT NOT NULL
                      CHECK (category IN (
                          'allergy',
                          'dietary_restriction',
                          'dislike',
                          'cuisine',
                          'budget',
                          'cooking_time'
                      )),
    value        TEXT NOT NULL
                      CHECK (value = trim(value) AND value <> ''),
    strength     INTEGER
                      CHECK (
                          (kind = 'hard' AND strength IS NULL)
                          OR (
                              kind = 'soft'
                              AND typeof(strength) = 'integer'
                              AND strength BETWEEN 1 AND 5
                          )
                      ),
    created_at   TEXT NOT NULL
                      DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

    FOREIGN KEY (household_id)
        REFERENCES household (id)
        ON DELETE CASCADE,
    FOREIGN KEY (household_id, member_id)
        REFERENCES member (household_id, id)
        ON DELETE CASCADE
);

-- Future auth identities should reference member(id).

COMMIT;
