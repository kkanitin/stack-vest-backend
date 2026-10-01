-- One row per user per UTC calendar day: the total USD value of everything the user
-- held, across all portfolios, as last recorded that day. Keyed by user (not portfolio)
-- so deleting a portfolio does not rewrite the totals of days already recorded.
CREATE TABLE stackvest.user_value_snapshots (
    user_id       UUID          NOT NULL REFERENCES stackvest.users(id) ON DELETE CASCADE,
    snapshot_date DATE          NOT NULL,
    value_usd     NUMERIC(20,4) NOT NULL CHECK (value_usd >= 0),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, snapshot_date)
);
