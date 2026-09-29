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

CREATE TABLE IF NOT EXISTS away_night (
    household_id TEXT NOT NULL
                      CHECK (household_id <> ''),
    member_id    TEXT NOT NULL
                      CHECK (member_id <> ''),
    night        TEXT NOT NULL
                      CHECK (typeof(night) = 'text' AND night IS date(night)),
    created_at   TEXT NOT NULL
                      DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

    PRIMARY KEY (member_id, night),
    FOREIGN KEY (household_id)
        REFERENCES household (id)
        ON DELETE CASCADE,
    FOREIGN KEY (household_id, member_id)
        REFERENCES member (household_id, id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS away_night_household_night
    ON away_night (household_id, night);

CREATE TABLE IF NOT EXISTS web_identity (
    email      TEXT PRIMARY KEY
                    CHECK (email = lower(trim(email)) AND email <> ''),
    member_id  TEXT NOT NULL UNIQUE
                    CHECK (member_id <> ''),
    created_at INTEGER NOT NULL
                    DEFAULT (unixepoch()),

    FOREIGN KEY (member_id)
        REFERENCES member (id)
        ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS magic_link (
    token_hash BLOB NOT NULL PRIMARY KEY
                    CHECK (typeof(token_hash) = 'blob' AND length(token_hash) = 32),
    member_id  TEXT NOT NULL
                    CHECK (member_id <> ''),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
                    CHECK (expires_at > created_at),
    used_at    INTEGER,

    FOREIGN KEY (member_id)
        REFERENCES member (id)
        ON DELETE CASCADE
);

-- Lifecycle rules live in triggers rather than CHECK constraints because
-- CREATE TABLE IF NOT EXISTS cannot add constraints to an existing table,
-- whereas these triggers are installed on existing databases as well.
CREATE TRIGGER IF NOT EXISTS magic_link_valid_insert
BEFORE INSERT ON magic_link
WHEN typeof(NEW.token_hash) <> 'blob'
    OR length(NEW.token_hash) <> 32
    OR typeof(NEW.created_at) <> 'integer'
    OR typeof(NEW.expires_at) <> 'integer'
    OR NEW.expires_at <= NEW.created_at
    OR (NEW.used_at IS NOT NULL AND (
        typeof(NEW.used_at) <> 'integer'
        OR NEW.used_at < NEW.created_at
        OR NEW.used_at >= NEW.expires_at
    ))
BEGIN
    SELECT RAISE(ABORT, 'invalid magic link lifecycle');
END;

CREATE TRIGGER IF NOT EXISTS magic_link_valid_update
BEFORE UPDATE ON magic_link
WHEN typeof(NEW.token_hash) <> 'blob'
    OR length(NEW.token_hash) <> 32
    OR typeof(NEW.created_at) <> 'integer'
    OR typeof(NEW.expires_at) <> 'integer'
    OR NEW.expires_at <= NEW.created_at
    OR (NEW.used_at IS NOT NULL AND (
        typeof(NEW.used_at) <> 'integer'
        OR NEW.used_at < NEW.created_at
        OR NEW.used_at >= NEW.expires_at
    ))
BEGIN
    SELECT RAISE(ABORT, 'invalid magic link lifecycle');
END;

CREATE TRIGGER IF NOT EXISTS magic_link_single_use
BEFORE UPDATE OF used_at ON magic_link
WHEN OLD.used_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'magic link has already been used');
END;

-- The triggers only guard future writes. Rows written before they existed
-- may break the same rules, and consuming one would then abort inside the
-- update trigger. Links are short-lived and reissued on request, so discard
-- any such row rather than repair it. After the first upgrade this matches
-- nothing, keeping the schema safe to reapply.
DELETE FROM magic_link
WHERE typeof(token_hash) <> 'blob'
    OR length(token_hash) <> 32
    OR typeof(created_at) <> 'integer'
    OR typeof(expires_at) <> 'integer'
    OR expires_at <= created_at
    OR (used_at IS NOT NULL AND (
        typeof(used_at) <> 'integer'
        OR used_at < created_at
        OR used_at >= expires_at
    ));

CREATE TABLE IF NOT EXISTS web_session (
    token_hash BLOB PRIMARY KEY
                    CHECK (length(token_hash) = 32),
    member_id  TEXT NOT NULL
                    CHECK (member_id <> ''),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
                    CHECK (expires_at > created_at),

    FOREIGN KEY (member_id)
        REFERENCES member (id)
        ON DELETE CASCADE
);

-- Telegram identities are linked by a member first issuing a one-time code
-- from an authenticated interface and then sending it to the bot. These are
-- new tables, so their rules are CHECK constraints rather than triggers.
CREATE TABLE IF NOT EXISTS telegram_identity (
    telegram_user_id INTEGER NOT NULL PRIMARY KEY
                          CHECK (typeof(telegram_user_id) = 'integer' AND telegram_user_id > 0),
    member_id        TEXT NOT NULL UNIQUE
                          CHECK (member_id <> ''),
    linked_at        INTEGER NOT NULL
                          CHECK (typeof(linked_at) = 'integer'),

    FOREIGN KEY (member_id)
        REFERENCES member (id)
        ON DELETE CASCADE
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS telegram_link_code (
    code_hash  BLOB NOT NULL PRIMARY KEY
                    CHECK (typeof(code_hash) = 'blob' AND length(code_hash) = 32),
    member_id  TEXT NOT NULL
                    CHECK (member_id <> ''),
    created_at INTEGER NOT NULL
                    CHECK (typeof(created_at) = 'integer'),
    expires_at INTEGER NOT NULL
                    CHECK (typeof(expires_at) = 'integer' AND expires_at > created_at),
    used_at    INTEGER
                    CHECK (used_at IS NULL OR (
                        typeof(used_at) = 'integer'
                        AND used_at >= created_at
                        AND used_at < expires_at
                    )),

    FOREIGN KEY (member_id)
        REFERENCES member (id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS telegram_link_code_member
    ON telegram_link_code (member_id);

CREATE TRIGGER IF NOT EXISTS telegram_link_code_single_use
BEFORE UPDATE OF used_at ON telegram_link_code
WHEN OLD.used_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'telegram link code has already been used');
END;

-- Invalid link codes sent by each Telegram user within the current window,
-- so the service can refuse further guesses. Senders are usually unlinked,
-- so this table deliberately has no foreign key.
CREATE TABLE IF NOT EXISTS telegram_link_attempt (
    telegram_user_id  INTEGER NOT NULL PRIMARY KEY
                           CHECK (typeof(telegram_user_id) = 'integer' AND telegram_user_id > 0),
    failed_attempts   INTEGER NOT NULL
                           CHECK (typeof(failed_attempts) = 'integer' AND failed_attempts > 0),
    window_started_at INTEGER NOT NULL
                           CHECK (typeof(window_started_at) = 'integer')
) WITHOUT ROWID;

COMMIT;
