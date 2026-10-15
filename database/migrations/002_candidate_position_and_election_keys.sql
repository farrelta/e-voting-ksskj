-- 1) Stable candidate ordering.
--    The homomorphic vote encoding needs candidate index 0..n-1, but candidates
--    are identified by UUID. `position` gives every candidate a fixed slot.
--    Positions are assigned in creation order and may have gaps after a delete;
--    the voting code must use the candidate's RANK when ordered by position
--    (candidates can't change once voting starts, so the rank is stable).
ALTER TABLE evoting.candidates
    ADD COLUMN IF NOT EXISTS position integer;

UPDATE evoting.candidates c
SET position = r.rn
FROM (
    SELECT id, (row_number() OVER (PARTITION BY election_id ORDER BY name) - 1) AS rn
    FROM evoting.candidates
) r
WHERE c.id = r.id AND c.position IS NULL;

ALTER TABLE evoting.candidates
    ALTER COLUMN position SET NOT NULL;

CREATE OR REPLACE FUNCTION evoting.assign_candidate_position()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.position IS NULL THEN
        SELECT COALESCE(MAX(position) + 1, 0)
        INTO NEW.position
        FROM evoting.candidates
        WHERE election_id = NEW.election_id;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS candidates_assign_position ON evoting.candidates;
CREATE TRIGGER candidates_assign_position
    BEFORE INSERT ON evoting.candidates
    FOR EACH ROW EXECUTE FUNCTION evoting.assign_candidate_position();

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'unique_candidate_position'
    ) THEN
        ALTER TABLE evoting.candidates
            ADD CONSTRAINT unique_candidate_position UNIQUE (election_id, position);
    END IF;
END;
$$;

-- 2) Per-election cryptographic keys.
--    Public keys live on the election row (safe to read). Secret keys live in a
--    separate table so a normal election query can never select them. The *_enc
--    columns must hold ciphertext: encrypting them at rest is the security
--    owner's job (e.g. envelope encryption with a master key from the environment).
ALTER TABLE evoting.elections
    ADD COLUMN IF NOT EXISTS he_public_key  bytea,
    ADD COLUMN IF NOT EXISTS dsa_public_key bytea,
    ADD COLUMN IF NOT EXISTS kem_public_key bytea;

CREATE TABLE IF NOT EXISTS evoting.election_secrets (
    election_id         uuid PRIMARY KEY
                        REFERENCES evoting.elections(id) ON DELETE CASCADE,
    he_secret_key_enc   bytea NOT NULL,
    dsa_private_key_enc bytea NOT NULL,
    kem_private_key_enc bytea NOT NULL,
    created_at          timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER TABLE evoting.election_secrets OWNER TO evoting_app;
