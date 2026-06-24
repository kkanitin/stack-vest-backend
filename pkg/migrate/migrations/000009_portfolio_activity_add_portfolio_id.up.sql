-- Ensure a default portfolio exists for any user that has activity but no positions.
INSERT INTO stackvest.portfolios (user_id, name)
SELECT DISTINCT pa.user_id, 'Default'
FROM stackvest.portfolio_activity pa
WHERE NOT EXISTS (
    SELECT 1 FROM stackvest.portfolios p
    WHERE p.user_id = pa.user_id AND p.name = 'Default'
);

ALTER TABLE stackvest.portfolio_activity
    ADD COLUMN portfolio_id UUID REFERENCES stackvest.portfolios(id) ON DELETE CASCADE;

UPDATE stackvest.portfolio_activity pa
SET portfolio_id = p.id
FROM stackvest.portfolios p
WHERE p.user_id = pa.user_id AND p.name = 'Default';

ALTER TABLE stackvest.portfolio_activity
    ALTER COLUMN portfolio_id SET NOT NULL;

DROP INDEX stackvest.portfolio_activity_user_id_occurred_at_idx;

ALTER TABLE stackvest.portfolio_activity
    DROP COLUMN user_id;

CREATE INDEX portfolio_activity_portfolio_id_occurred_at_idx
    ON stackvest.portfolio_activity (portfolio_id, occurred_at DESC);
