-- Annual volatility (percent) the user typed for the goal's Monte Carlo. NULL means not set;
-- existing goals stay NULL.
ALTER TABLE stackvest.goals
    ADD COLUMN volatility NUMERIC(9,4) NULL CHECK (volatility >= 0 AND volatility <= 100);
