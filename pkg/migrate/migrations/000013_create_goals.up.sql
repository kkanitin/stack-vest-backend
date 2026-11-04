CREATE TABLE stackvest.goals (
    id                  UUID          NOT NULL DEFAULT gen_random_uuid(),
    user_id             UUID          NOT NULL REFERENCES stackvest.users(id) ON DELETE CASCADE,
    -- Deleting a portfolio keeps the goal and clears the link.
    portfolio_id        UUID          NULL REFERENCES stackvest.portfolios(id) ON DELETE SET NULL,
    name                TEXT          NOT NULL,
    kind                TEXT          NOT NULL CHECK (kind IN ('projection', 'target')),
    currency            TEXT          NOT NULL DEFAULT 'USD',
    starting_amount     NUMERIC(20,2) NOT NULL DEFAULT 0 CHECK (starting_amount >= 0),
    monthly_amount      NUMERIC(20,2) NULL CHECK (monthly_amount >= 0),
    target_amount       NUMERIC(20,2) NULL CHECK (target_amount > 0),
    years               INTEGER       NOT NULL CHECK (years > 0),
    expected_return     NUMERIC(9,4)  NOT NULL,
    contribution_growth NUMERIC(9,4)  NULL,
    inflation           NUMERIC(9,4)  NULL,
    created_at          TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id)
);

CREATE INDEX goals_user_id_idx
    ON stackvest.goals (user_id);

CREATE INDEX goals_portfolio_id_idx
    ON stackvest.goals (portfolio_id);
