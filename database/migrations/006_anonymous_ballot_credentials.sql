BEGIN;

ALTER TABLE evoting.anonymous_ballots
    ADD COLUMN IF NOT EXISTS credential bytea;

-- The earlier build did not accept anonymous ballots. If test data was
-- inserted manually, require it to be reconciled before enabling the pilot.
ALTER TABLE evoting.anonymous_ballots ALTER COLUMN credential SET NOT NULL;

COMMIT;
