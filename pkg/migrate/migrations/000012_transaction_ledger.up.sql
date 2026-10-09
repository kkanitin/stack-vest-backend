-- Transaction ledger. portfolio_positions becomes a cache derived from this table
-- (rewritten in the same DB transaction as every ledger change).
CREATE TABLE stackvest.portfolio_transactions (
    id           UUID          NOT NULL DEFAULT gen_random_uuid(),
    portfolio_id UUID          NOT NULL REFERENCES stackvest.portfolios(id) ON DELETE CASCADE,
    symbol       TEXT          NOT NULL,
    name         TEXT          NOT NULL,
    side         TEXT          NOT NULL CHECK (side IN ('buy', 'sell')),
    quantity     NUMERIC(20,8) NOT NULL CHECK (quantity > 0),
    price        NUMERIC(20,8) NOT NULL CHECK (price >= 0),
    fee          NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (fee >= 0),
    note         TEXT,
    trade_date   DATE          NOT NULL,
    is_opening   BOOLEAN       NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id)
);

CREATE INDEX portfolio_transactions_portfolio_symbol_date_idx
    ON stackvest.portfolio_transactions (portfolio_id, symbol, trade_date, created_at);

ALTER TABLE stackvest.portfolio_positions
    ADD COLUMN realised_pnl NUMERIC(20,8) NOT NULL DEFAULT 0;

-- shares = 0 now means a closed holding.
ALTER TABLE stackvest.portfolio_positions
    DROP CONSTRAINT portfolio_positions_shares_check;
ALTER TABLE stackvest.portfolio_positions
    ADD CONSTRAINT portfolio_positions_shares_check CHECK (shares >= 0);

-- Carry-over: one opening buy per existing position, so replaying the ledger gives back
-- exactly the stored shares and avg_cost. created_at = added_at keeps the activity feed
-- from opening with a burst of "Opening balance" rows dated today.
INSERT INTO stackvest.portfolio_transactions
    (portfolio_id, symbol, name, side, quantity, price, fee, trade_date, is_opening, created_at)
SELECT portfolio_id, symbol, name, 'buy', shares, avg_cost, 0, (added_at AT TIME ZONE 'UTC')::date, TRUE, added_at
FROM stackvest.portfolio_positions;
