CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;

CREATE TABLE IF NOT EXISTS evoting.election_keys (
    election_id uuid PRIMARY KEY REFERENCES evoting.elections(id) ON DELETE CASCADE,
    kem_public_key bytea NOT NULL,
    kem_private_key bytea NOT NULL,
    dsa_public_key bytea NOT NULL,
    dsa_private_key bytea NOT NULL,
    he_public_key bytea NOT NULL,
    he_secret_key_fingerprint character varying(64) NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS evoting.election_results (
    election_id uuid PRIMARY KEY REFERENCES evoting.elections(id) ON DELETE CASCADE,
    total_ballots integer NOT NULL DEFAULT 0,
    results jsonb NOT NULL DEFAULT '[]'::jsonb,
    tallied_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);
