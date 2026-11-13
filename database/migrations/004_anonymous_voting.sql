BEGIN;

ALTER TABLE evoting.elections
    ADD COLUMN IF NOT EXISTS voting_mode text NOT NULL DEFAULT 'receipt';

DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_constraint
                   WHERE conrelid = 'evoting.elections'::regclass AND conname = 'elections_voting_mode_check') THEN
        ALTER TABLE evoting.elections ADD CONSTRAINT elections_voting_mode_check
            CHECK (voting_mode IN ('receipt', 'anonymous_v1'));
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS evoting.credential_issuances (
    election_id uuid NOT NULL,
    user_id uuid NOT NULL,
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    blind_response bytea NOT NULL,
    PRIMARY KEY (election_id, user_id),
    FOREIGN KEY (election_id, user_id)
        REFERENCES evoting.voter_eligibility (election_id, user_id)
);

CREATE TABLE IF NOT EXISTS evoting.anonymous_ballots (
    election_id uuid NOT NULL REFERENCES evoting.elections(id),
    token_digest bytea NOT NULL CHECK (octet_length(token_digest) = 32),
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    encrypted_vote bytea NOT NULL,
    proof bytea NOT NULL,
    ballot_hash text NOT NULL UNIQUE CHECK (ballot_hash ~ '^[0-9a-f]{64}$'),
    PRIMARY KEY (election_id, token_digest)
);

COMMIT;
