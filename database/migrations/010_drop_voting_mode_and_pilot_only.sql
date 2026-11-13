BEGIN;

LOCK TABLE evoting.elections, evoting.anonymous_issuer_keys IN ACCESS EXCLUSIVE MODE;

-- Remove the credential-only keys before the issuer record becomes the
-- condition for voting. Keep the elections and their historical records.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'evoting' AND table_name = 'anonymous_issuer_keys'
          AND column_name = 'pilot_only'
    ) THEN
        IF EXISTS (
            SELECT 1 FROM evoting.anonymous_issuer_keys k
            JOIN evoting.anonymous_ballots b ON b.election_id = k.election_id
            WHERE k.pilot_only
        ) THEN
            RAISE EXCEPTION 'Credential-only election has recorded ballots. Reconcile its records before retiring its issuer key.';
        END IF;

        DELETE FROM evoting.anonymous_issuer_keys WHERE pilot_only;
    END IF;
END;
$$;

UPDATE evoting.elections e
SET status = 'completed'
WHERE status <> 'completed'
  AND NOT EXISTS (
      SELECT 1 FROM evoting.anonymous_issuer_keys k WHERE k.election_id = e.id
  );

ALTER TABLE evoting.elections DROP COLUMN IF EXISTS voting_mode;
ALTER TABLE evoting.anonymous_issuer_keys DROP COLUMN IF EXISTS pilot_only;

COMMIT;
