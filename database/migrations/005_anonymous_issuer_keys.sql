BEGIN;

CREATE TABLE IF NOT EXISTS evoting.anonymous_issuer_keys (
    election_id uuid PRIMARY KEY REFERENCES evoting.elections(id) ON DELETE CASCADE,
    public_key bytea NOT NULL,
    private_key_enc bytea NOT NULL,
    pilot_only boolean NOT NULL
);

COMMIT;
