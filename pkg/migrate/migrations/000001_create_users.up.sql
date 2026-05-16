CREATE SCHEMA IF NOT EXISTS stackvest;

CREATE TABLE IF NOT EXISTS stackvest.users
(
    id
    UUID
    PRIMARY
    KEY
    DEFAULT
    gen_random_uuid
(
),
    google_id TEXT,
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    picture TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
    );
