BEGIN;

-- Preserve election metadata and any published tally. Close receipt elections
-- because their ballots and verification codes are about to be removed.
UPDATE evoting.elections
SET status = 'completed'
WHERE voting_mode = 'receipt' AND status <> 'completed';

ALTER TABLE evoting.voter_eligibility
    DROP COLUMN IF EXISTS receipt_id,
    DROP COLUMN IF EXISTS has_voted;

DROP TABLE IF EXISTS evoting.encrypted_ballots;
DROP TABLE IF EXISTS evoting.receipts;

COMMIT;
