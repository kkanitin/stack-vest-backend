CREATE UNIQUE INDEX IF NOT EXISTS users_google_id_key
    ON stackvest.users (google_id);
