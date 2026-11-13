BEGIN;

LOCK TABLE evoting.elections IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF to_regclass('evoting.election_keys') IS NULL OR
       to_regclass('evoting.election_results') IS NULL THEN
        RAISE EXCEPTION 'Apply 002_election_crypto_and_results.sql before this migration';
    END IF;

    LOCK TABLE evoting.election_keys IN SHARE MODE;

    IF to_regclass('evoting.election_secrets') IS NOT NULL THEN
        LOCK TABLE evoting.election_secrets IN ACCESS EXCLUSIVE MODE;
        IF EXISTS (SELECT 1 FROM evoting.election_secrets) THEN
            RAISE EXCEPTION 'Legacy election_secrets contains data. Back up and reconcile it before proceeding.';
        END IF;
    END IF;

    IF EXISTS (
        SELECT 1 FROM evoting.elections e
        LEFT JOIN evoting.election_keys k ON k.election_id = e.id
        WHERE (to_jsonb(e)->>'he_public_key' IS NOT NULL AND
               to_jsonb(e)->>'he_public_key' IS DISTINCT FROM to_jsonb(k)->>'he_public_key')
           OR (to_jsonb(e)->>'dsa_public_key' IS NOT NULL AND
               to_jsonb(e)->>'dsa_public_key' IS DISTINCT FROM to_jsonb(k)->>'dsa_public_key')
           OR (to_jsonb(e)->>'kem_public_key' IS NOT NULL AND
               to_jsonb(e)->>'kem_public_key' IS DISTINCT FROM to_jsonb(k)->>'kem_public_key')
    ) THEN
        RAISE EXCEPTION 'Legacy public keys differ from election_keys. Back up and reconcile them before proceeding.';
    END IF;
END;
$$;

DROP TABLE IF EXISTS evoting.election_secrets;
ALTER TABLE evoting.elections
    DROP COLUMN IF EXISTS he_public_key,
    DROP COLUMN IF EXISTS dsa_public_key,
    DROP COLUMN IF EXISTS kem_public_key;

COMMIT;
