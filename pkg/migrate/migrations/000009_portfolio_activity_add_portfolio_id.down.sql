ALTER TABLE stackvest.portfolio_activity
    ADD COLUMN user_id UUID REFERENCES stackvest.users(id) ON DELETE CASCADE;

UPDATE stackvest.portfolio_activity pa
SET user_id = p.user_id
FROM stackvest.portfolios p
WHERE p.id = pa.portfolio_id;

ALTER TABLE stackvest.portfolio_activity
    ALTER COLUMN user_id SET NOT NULL;

DROP INDEX stackvest.portfolio_activity_portfolio_id_occurred_at_idx;

ALTER TABLE stackvest.portfolio_activity
    DROP COLUMN portfolio_id;

CREATE INDEX portfolio_activity_user_id_occurred_at_idx
    ON stackvest.portfolio_activity (user_id, occurred_at DESC);
