-- Closed holdings (shares = 0) cannot exist under the old check; drop them first.
-- Realised P&L and the transaction history are lost.
DELETE FROM stackvest.portfolio_positions WHERE shares = 0;

ALTER TABLE stackvest.portfolio_positions
    DROP CONSTRAINT portfolio_positions_shares_check;
ALTER TABLE stackvest.portfolio_positions
    ADD CONSTRAINT portfolio_positions_shares_check CHECK (shares > 0);

ALTER TABLE stackvest.portfolio_positions
    DROP COLUMN realised_pnl;

DROP TABLE stackvest.portfolio_transactions;
