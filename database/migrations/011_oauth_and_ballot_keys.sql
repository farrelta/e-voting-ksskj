BEGIN;

CREATE TABLE IF NOT EXISTS evoting.oauth_identities (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    user_id uuid NOT NULL,
    provider character varying(32) NOT NULL,
    subject character varying(255) NOT NULL,
    email character varying(255) NOT NULL,
    last_login_at timestamp with time zone,
    CONSTRAINT unique_provider_subject UNIQUE (provider, subject),
    CONSTRAINT unique_user_provider UNIQUE (user_id, provider),
    CONSTRAINT fk_oauth_identity_user FOREIGN KEY (user_id)
        REFERENCES evoting.users(id) ON DELETE CASCADE
);

ALTER TABLE evoting.oauth_identities OWNER TO evoting_app;
ALTER TABLE evoting.oauth_identities DROP COLUMN IF EXISTS created_at;
DROP INDEX IF EXISTS evoting.oauth_identities_user_id_idx;

-- Anonymous elections already use these fields for their ElGamal tally keys.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'evoting' AND table_name = 'election_keys'
          AND column_name = 'he_public_key'
    ) THEN
        ALTER TABLE evoting.election_keys RENAME COLUMN he_public_key TO ballot_public_key;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'evoting' AND table_name = 'election_keys'
          AND column_name = 'he_secret_key_fingerprint'
    ) THEN
        ALTER TABLE evoting.election_keys RENAME COLUMN he_secret_key_fingerprint TO tally_key_fingerprint;
    END IF;

    -- PostgreSQL 18 stores NOT NULL constraints under their original names.
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'evoting.election_keys'::regclass
          AND conname = 'election_keys_he_public_key_not_null'
    ) THEN
        ALTER TABLE evoting.election_keys RENAME CONSTRAINT election_keys_he_public_key_not_null
            TO election_keys_ballot_public_key_not_null;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'evoting.election_keys'::regclass
          AND conname = 'election_keys_he_secret_key_fingerprint_not_null'
    ) THEN
        ALTER TABLE evoting.election_keys RENAME CONSTRAINT election_keys_he_secret_key_fingerprint_not_null
            TO election_keys_tally_key_fingerprint_not_null;
    END IF;
END;
$$;

ALTER TABLE evoting.election_keys
    DROP COLUMN IF EXISTS kem_public_key,
    DROP COLUMN IF EXISTS kem_private_key,
    DROP COLUMN IF EXISTS dsa_public_key,
    DROP COLUMN IF EXISTS dsa_private_key;

COMMIT;
