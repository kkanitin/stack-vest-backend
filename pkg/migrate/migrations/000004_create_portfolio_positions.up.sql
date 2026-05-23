CREATE TABLE stackvest.portfolio_positions (
    id        UUID          NOT NULL DEFAULT gen_random_uuid(),
    user_id   UUID          NOT NULL REFERENCES stackvest.users(id) ON DELETE CASCADE,
    symbol    TEXT          NOT NULL,
    name      TEXT          NOT NULL,
    shares    NUMERIC(20,8) NOT NULL CHECK (shares > 0),
    avg_cost  NUMERIC(20,8) NOT NULL CHECK (avg_cost >= 0),
    added_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    UNIQUE (user_id, symbol)
);

CREATE INDEX portfolio_positions_user_id_idx
    ON stackvest.portfolio_positions (user_id);
