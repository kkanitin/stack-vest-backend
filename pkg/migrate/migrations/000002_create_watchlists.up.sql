CREATE TABLE IF NOT EXISTS stackvest.watchlists (
    id       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id  UUID        NOT NULL REFERENCES stackvest.users(id) ON DELETE CASCADE,
    symbol   TEXT        NOT NULL,
    name     TEXT        NOT NULL,
    type     TEXT        NOT NULL DEFAULT '',
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, symbol)
);

CREATE INDEX IF NOT EXISTS idx_watchlists_user_id ON stackvest.watchlists(user_id);
