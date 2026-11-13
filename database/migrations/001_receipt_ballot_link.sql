CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;

ALTER TABLE evoting.voter_eligibility
    ADD COLUMN IF NOT EXISTS receipt_id uuid;

ALTER TABLE evoting.encrypted_ballots
    ADD COLUMN IF NOT EXISTS receipt_id uuid;

CREATE UNIQUE INDEX IF NOT EXISTS voter_eligibility_receipt_id_key
    ON evoting.voter_eligibility (receipt_id);

CREATE UNIQUE INDEX IF NOT EXISTS encrypted_ballots_receipt_id_key
    ON evoting.encrypted_ballots (receipt_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_eligibility_receipt'
    ) THEN
        ALTER TABLE evoting.voter_eligibility
            ADD CONSTRAINT fk_eligibility_receipt
            FOREIGN KEY (receipt_id) REFERENCES evoting.receipts(id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_ballot_receipt'
    ) THEN
        ALTER TABLE evoting.encrypted_ballots
            ADD CONSTRAINT fk_ballot_receipt
            FOREIGN KEY (receipt_id) REFERENCES evoting.receipts(id);
    END IF;
END;
$$;
