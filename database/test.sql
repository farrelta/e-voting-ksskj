\set ON_ERROR_STOP on

-- ============================================================
-- E-Voting PostgreSQL Database Test Suite
-- ============================================================
-- Purpose:
--   Verify important database constraints and voting invariants.
--
-- The entire test runs inside one transaction and is rolled back
-- at the end, so the test data is not kept in the database.
--
-- Run from the project root with:
--   psql -U evoting_app -d evoting_db -h localhost -f database/test.sql
-- ============================================================

BEGIN;

\echo ''
\echo '============================================================'
\echo ' E-VOTING DATABASE TEST SUITE'
\echo '============================================================'

-- ------------------------------------------------------------
-- 1. UUID auto-generation
-- ------------------------------------------------------------
\echo ''
\echo '=== 1. UUID DEFAULT TEST ==='

DO $$
DECLARE
    generated_id uuid;
BEGIN
    INSERT INTO evoting.users
        (email, password_hash, role, is_verified)
    VALUES
        ('dbtest.admin@example.test', 'fake_hash', 'admin', true)
    RETURNING id INTO generated_id;

    IF generated_id IS NULL THEN
        RAISE EXCEPTION 'UUID generation failed for evoting.users';
    END IF;

    RAISE NOTICE 'PASS: users.id auto-generated: %', generated_id;
END $$;

-- ------------------------------------------------------------
-- 2. User insert
-- ------------------------------------------------------------
\echo ''
\echo '=== 2. USER INSERT TEST ==='

INSERT INTO evoting.users
    (email, password_hash, role, is_verified)
VALUES
    ('dbtest.voter@example.test', 'fake_hash', 'voter', true)
RETURNING id, email, role;

\echo 'PASS: voter inserted successfully.'

-- ------------------------------------------------------------
-- 3. Unique email
-- ------------------------------------------------------------
\echo ''
\echo '=== 3. UNIQUE EMAIL TEST ==='

DO $$
BEGIN
    BEGIN
        INSERT INTO evoting.users
            (email, password_hash, role, is_verified)
        VALUES
            ('dbtest.voter@example.test', 'another_hash', 'voter', true);

        RAISE EXCEPTION 'Duplicate email was accepted';
    EXCEPTION
        WHEN unique_violation THEN
            RAISE NOTICE 'PASS: duplicate user email rejected.';
    END;
END $$;

-- ------------------------------------------------------------
-- 4. Election insert and created_by foreign key
-- ------------------------------------------------------------
\echo ''
\echo '=== 4. ELECTION INSERT + FK TEST ==='

DO $$
DECLARE
    v_admin_id uuid;
    v_election_id uuid;
BEGIN
    SELECT id
      INTO v_admin_id
      FROM evoting.users
     WHERE email = 'dbtest.admin@example.test';

    INSERT INTO evoting.elections
        (
            title,
            description,
            registration_start,
            registration_end,
            voting_start,
            voting_end,
            status,
            created_by
        )
    VALUES
        (
            'Database Test Election',
            'Automated PostgreSQL database test',
            NOW(),
            NOW() + INTERVAL '1 day',
            NOW(),
            NOW() + INTERVAL '2 days',
            'open',
            v_admin_id
        )
    RETURNING id INTO v_election_id;

    IF v_election_id IS NULL THEN
        RAISE EXCEPTION 'Election ID was not generated';
    END IF;

    RAISE NOTICE 'PASS: election inserted with generated UUID: %', v_election_id;
END $$;

-- ------------------------------------------------------------
-- 5. Candidate insert
-- ------------------------------------------------------------
\echo ''
\echo '=== 5. CANDIDATE INSERT TEST ==='

INSERT INTO evoting.candidates
    (election_id, name, description)
SELECT
    e.id,
    v.name,
    v.description
FROM evoting.elections AS e
CROSS JOIN (
    VALUES
        ('Candidate A', 'Test candidate A'),
        ('Candidate B', 'Test candidate B')
) AS v(name, description)
WHERE e.title = 'Database Test Election';

DO $$
DECLARE
    v_candidate_count integer;
BEGIN
    SELECT COUNT(*)
      INTO v_candidate_count
      FROM evoting.candidates AS c
      JOIN evoting.elections AS e
        ON e.id = c.election_id
     WHERE e.title = 'Database Test Election';

    IF v_candidate_count <> 2 THEN
        RAISE EXCEPTION 'Expected 2 candidates, found %', v_candidate_count;
    END IF;

    RAISE NOTICE 'PASS: 2 candidates inserted.';
END $$;

-- ------------------------------------------------------------
-- 6. Duplicate candidate name in same election
-- ------------------------------------------------------------
\echo ''
\echo '=== 6. DUPLICATE CANDIDATE TEST ==='

DO $$
DECLARE
    v_election_id uuid;
BEGIN
    SELECT id
      INTO v_election_id
      FROM evoting.elections
     WHERE title = 'Database Test Election';

    BEGIN
        INSERT INTO evoting.candidates
            (election_id, name, description)
        VALUES
            (v_election_id, 'Candidate A', 'Duplicate candidate');

        RAISE EXCEPTION 'Duplicate candidate name was accepted';
    EXCEPTION
        WHEN unique_violation THEN
            RAISE NOTICE 'PASS: duplicate candidate name rejected.';
    END;
END $$;

-- ------------------------------------------------------------
-- 7. Voter eligibility
-- ------------------------------------------------------------
\echo ''
\echo '=== 7. VOTER ELIGIBILITY TEST ==='

INSERT INTO evoting.voter_eligibility
    (election_id, user_id)
SELECT
    e.id,
    u.id
FROM evoting.elections AS e
CROSS JOIN evoting.users AS u
WHERE e.title = 'Database Test Election'
  AND u.email = 'dbtest.voter@example.test';

DO $$
DECLARE
    v_has_voted boolean;
BEGIN
    SELECT ve.has_voted
      INTO v_has_voted
      FROM evoting.voter_eligibility AS ve
      JOIN evoting.elections AS e
        ON e.id = ve.election_id
      JOIN evoting.users AS u
        ON u.id = ve.user_id
     WHERE e.title = 'Database Test Election'
       AND u.email = 'dbtest.voter@example.test';

    IF v_has_voted IS DISTINCT FROM false THEN
        RAISE EXCEPTION 'New voter should have has_voted = false';
    END IF;

    RAISE NOTICE 'PASS: voter eligibility created with has_voted = false.';
END $$;

-- ------------------------------------------------------------
-- 8. Duplicate voter registration
-- ------------------------------------------------------------
\echo ''
\echo '=== 8. DUPLICATE VOTER REGISTRATION TEST ==='

DO $$
DECLARE
    v_election_id uuid;
    v_voter_id uuid;
BEGIN
    SELECT id
      INTO v_election_id
      FROM evoting.elections
     WHERE title = 'Database Test Election';

    SELECT id
      INTO v_voter_id
      FROM evoting.users
     WHERE email = 'dbtest.voter@example.test';

    BEGIN
        INSERT INTO evoting.voter_eligibility
            (election_id, user_id)
        VALUES
            (v_election_id, v_voter_id);

        RAISE EXCEPTION 'Duplicate voter registration was accepted';
    EXCEPTION
        WHEN unique_violation THEN
            RAISE NOTICE 'PASS: duplicate voter registration rejected.';
    END;
END $$;

-- ------------------------------------------------------------
-- 9. Encrypted ballot insert
-- ------------------------------------------------------------
\echo ''
\echo '=== 9. ENCRYPTED BALLOT INSERT TEST ==='

INSERT INTO evoting.encrypted_ballots
    (election_id, encrypted_vote, proof, ballot_hash)
SELECT
    e.id,
    decode('deadbeef', 'hex'),
    decode('cafebabe', 'hex'),
    'dbtest-ballot-hash-001'
FROM evoting.elections AS e
WHERE e.title = 'Database Test Election';

DO $$
DECLARE
    v_ballot_count integer;
BEGIN
    SELECT COUNT(*)
      INTO v_ballot_count
      FROM evoting.encrypted_ballots AS eb
      JOIN evoting.elections AS e
        ON e.id = eb.election_id
     WHERE e.title = 'Database Test Election';

    IF v_ballot_count <> 1 THEN
        RAISE EXCEPTION 'Expected 1 encrypted ballot, found %', v_ballot_count;
    END IF;

    RAISE NOTICE 'PASS: encrypted ballot inserted.';
END $$;

-- ------------------------------------------------------------
-- 10. Ballot hash uniqueness
-- ------------------------------------------------------------
\echo ''
\echo '=== 10. UNIQUE BALLOT HASH TEST ==='

DO $$
DECLARE
    v_election_id uuid;
BEGIN
    SELECT id
      INTO v_election_id
      FROM evoting.elections
     WHERE title = 'Database Test Election';

    BEGIN
        INSERT INTO evoting.encrypted_ballots
            (election_id, encrypted_vote, ballot_hash)
        VALUES
            (v_election_id, decode('11223344', 'hex'), 'dbtest-ballot-hash-001');

        RAISE EXCEPTION 'Duplicate ballot hash was accepted';
    EXCEPTION
        WHEN unique_violation THEN
            RAISE NOTICE 'PASS: duplicate ballot hash rejected.';
    END;
END $$;

-- ------------------------------------------------------------
-- 11. Receipt insert
-- ------------------------------------------------------------
\echo ''
\echo '=== 11. RECEIPT INSERT TEST ==='

INSERT INTO evoting.receipts
    (election_id, receipt_code)
SELECT
    e.id,
    'DBTEST-RECEIPT-001'
FROM evoting.elections AS e
WHERE e.title = 'Database Test Election';

\echo 'PASS: receipt inserted successfully.'

-- ------------------------------------------------------------
-- 12. Receipt uniqueness
-- ------------------------------------------------------------
\echo ''
\echo '=== 12. UNIQUE RECEIPT CODE TEST ==='

DO $$
DECLARE
    v_election_id uuid;
BEGIN
    SELECT id
      INTO v_election_id
      FROM evoting.elections
     WHERE title = 'Database Test Election';

    BEGIN
        INSERT INTO evoting.receipts
            (election_id, receipt_code)
        VALUES
            (v_election_id, 'DBTEST-RECEIPT-001');

        RAISE EXCEPTION 'Duplicate receipt code was accepted';
    EXCEPTION
        WHEN unique_violation THEN
            RAISE NOTICE 'PASS: duplicate receipt code rejected.';
    END;
END $$;

-- ------------------------------------------------------------
-- 13. One-vote enforcement
-- ------------------------------------------------------------
\echo ''
\echo '=== 13. ONE-VOTE ENFORCEMENT TEST ==='

DO $$
DECLARE
    v_election_id uuid;
    v_voter_id uuid;
    v_affected_rows integer;
BEGIN
    SELECT id
      INTO v_election_id
      FROM evoting.elections
     WHERE title = 'Database Test Election';

    SELECT id
      INTO v_voter_id
      FROM evoting.users
     WHERE email = 'dbtest.voter@example.test';

    UPDATE evoting.voter_eligibility AS ve
       SET has_voted = true
     WHERE ve.election_id = v_election_id
       AND ve.user_id = v_voter_id
       AND ve.has_voted = false;

    GET DIAGNOSTICS v_affected_rows = ROW_COUNT;

    IF v_affected_rows <> 1 THEN
        RAISE EXCEPTION
            'First vote should update exactly 1 row, updated %',
            v_affected_rows;
    END IF;

    UPDATE evoting.voter_eligibility AS ve
       SET has_voted = true
     WHERE ve.election_id = v_election_id
       AND ve.user_id = v_voter_id
       AND ve.has_voted = false;

    GET DIAGNOSTICS v_affected_rows = ROW_COUNT;

    IF v_affected_rows <> 0 THEN
        RAISE EXCEPTION
            'Second vote should update 0 rows, updated %',
            v_affected_rows;
    END IF;

    RAISE NOTICE 'PASS: one-vote enforcement works (1st = 1 row, 2nd = 0 rows).';
END $$;

-- ------------------------------------------------------------
-- 14. Anonymity schema checks
-- ------------------------------------------------------------
\echo ''
\echo '=== 14. BALLOT / VOTER SEPARATION TEST ==='

DO $$
DECLARE
    v_count integer;
BEGIN
    SELECT COUNT(*)
      INTO v_count
      FROM information_schema.columns
     WHERE table_schema = 'evoting'
       AND table_name = 'encrypted_ballots'
       AND column_name = 'user_id';

    IF v_count <> 0 THEN
        RAISE EXCEPTION 'encrypted_ballots unexpectedly contains user_id';
    END IF;

    SELECT COUNT(*)
      INTO v_count
      FROM information_schema.columns
     WHERE table_schema = 'evoting'
       AND table_name = 'receipts'
       AND column_name IN ('user_id', 'ballot_id');

    IF v_count <> 0 THEN
        RAISE EXCEPTION 'receipts unexpectedly contains user_id or ballot_id';
    END IF;

    RAISE NOTICE 'PASS: encrypted ballots have no user_id; receipts have no user_id/ballot_id.';
END $$;

-- ------------------------------------------------------------
-- 15. Foreign-key integrity
-- ------------------------------------------------------------
\echo ''
\echo '=== 15. FOREIGN KEY INTEGRITY TEST ==='

DO $$
BEGIN
    BEGIN
        INSERT INTO evoting.candidates
            (election_id, name, description)
        VALUES
            ('00000000-0000-0000-0000-000000000000', 'Invalid Candidate', 'Should fail');

        RAISE EXCEPTION 'Invalid election_id was accepted';
    EXCEPTION
        WHEN foreign_key_violation THEN
            RAISE NOTICE 'PASS: invalid election_id rejected by foreign key.';
    END;
END $$;

-- ------------------------------------------------------------
-- Summary
-- ------------------------------------------------------------
\echo ''
\echo '=== 16. TEST DATA SUMMARY ==='

SELECT
    (SELECT COUNT(*)
       FROM evoting.users
      WHERE email IN ('dbtest.admin@example.test', 'dbtest.voter@example.test')) AS test_users,
    (SELECT COUNT(*)
       FROM evoting.elections
      WHERE title = 'Database Test Election') AS test_elections,
    (SELECT COUNT(*)
       FROM evoting.candidates AS c
       JOIN evoting.elections AS e ON e.id = c.election_id
      WHERE e.title = 'Database Test Election') AS test_candidates,
    (SELECT COUNT(*)
       FROM evoting.voter_eligibility AS ve
       JOIN evoting.elections AS e ON e.id = ve.election_id
      WHERE e.title = 'Database Test Election') AS test_eligibility,
    (SELECT COUNT(*)
       FROM evoting.encrypted_ballots AS eb
       JOIN evoting.elections AS e ON e.id = eb.election_id
      WHERE e.title = 'Database Test Election') AS test_ballots,
    (SELECT COUNT(*)
       FROM evoting.receipts AS r
       JOIN evoting.elections AS e ON e.id = r.election_id
      WHERE e.title = 'Database Test Election') AS test_receipts;

\echo ''
\echo '============================================================'
\echo ' ALL TESTS PASSED'
\echo '============================================================'
\echo 'Rolling back test data...'

ROLLBACK;

\echo 'Test data removed.'

