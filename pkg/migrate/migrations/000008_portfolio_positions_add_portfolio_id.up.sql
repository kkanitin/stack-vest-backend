-- Create one default portfolio per user that currently holds positions.
INSERT INTO stackvest.portfolios (user_id, name)
SELECT DISTINCT user_id, 'Default'
FROM stackvest.portfolio_positions;

ALTER TABLE stackvest.portfolio_positions
    ADD COLUMN portfolio_id UUID REFERENCES stackvest.portfolios(id) ON DELETE CASCADE;

-- Move each existing position into its owner's default portfolio.
UPDATE stackvest.portfolio_positions pp
SET portfolio_id = p.id
FROM stackvest.portfolios p
WHERE p.user_id = pp.user_id AND p.name = 'Default';

ALTER TABLE stackvest.portfolio_positions
    ALTER COLUMN portfolio_id SET NOT NULL;

ALTER TABLE stackvest.portfolio_positions
    DROP CONSTRAINT portfolio_positions_user_id_symbol_key;

DROP INDEX stackvest.portfolio_positions_user_id_idx;

ALTER TABLE stackvest.portfolio_positions
    DROP COLUMN user_id;

ALTER TABLE stackvest.portfolio_positions
    ADD CONSTRAINT portfolio_positions_portfolio_id_symbol_key UNIQUE (portfolio_id, symbol);

CREATE INDEX portfolio_positions_portfolio_id_idx
    ON stackvest.portfolio_positions (portfolio_id);
