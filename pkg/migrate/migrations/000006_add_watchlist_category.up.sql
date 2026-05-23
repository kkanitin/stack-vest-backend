ALTER TABLE stackvest.watchlists
    ADD COLUMN category TEXT[] NOT NULL DEFAULT '{}';
