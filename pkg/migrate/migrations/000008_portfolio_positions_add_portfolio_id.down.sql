ALTER TABLE stackvest.portfolio_positions
    ADD COLUMN user_id UUID REFERENCES stackvest.users(id) ON DELETE CASCADE;

UPDATE stackvest.portfolio_positions pp
SET user_id = p.user_id
FROM stackvest.portfolios p
WHERE p.id = pp.portfolio_id;

ALTER TABLE stackvest.portfolio_positions
    ALTER COLUMN user_id SET NOT NULL;

ALTER TABLE stackvest.portfolio_positions
    DROP CONSTRAINT portfolio_positions_portfolio_id_symbol_key;

DROP INDEX stackvest.portfolio_positions_portfolio_id_idx;

ALTER TABLE stackvest.portfolio_positions
    DROP COLUMN portfolio_id;

ALTER TABLE stackvest.portfolio_positions
    ADD CONSTRAINT portfolio_positions_user_id_symbol_key UNIQUE (user_id, symbol);

CREATE INDEX portfolio_positions_user_id_idx
    ON stackvest.portfolio_positions (user_id);
