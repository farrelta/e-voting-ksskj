BEGIN;

-- Keep the mode of every existing election and all legacy receipts intact.
ALTER TABLE evoting.elections
    ALTER COLUMN voting_mode SET DEFAULT 'anonymous_v1';

COMMIT;
