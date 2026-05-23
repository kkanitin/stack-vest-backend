CREATE TABLE stackvest.portfolio_activity (
    id          UUID        NOT NULL DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES stackvest.users(id) ON DELETE CASCADE,
    symbol      TEXT,
    label       TEXT        NOT NULL,
    detail      TEXT        NOT NULL,
    tone        TEXT        NOT NULL CHECK (tone IN ('positive', 'negative', 'neutral')),
    badge       TEXT        NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id)
);

CREATE INDEX portfolio_activity_user_id_occurred_at_idx
    ON stackvest.portfolio_activity (user_id, occurred_at DESC);
