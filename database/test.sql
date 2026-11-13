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
-- 9. Anonymous ballot storage, uniqueness and voter separation
-- ------------------------------------------------------------
\echo ''
\echo '=== 9. ANONYMOUS BALLOT STORAGE TEST ==='

DO $$
DECLARE
    v_election_id uuid;
BEGIN
    SELECT id INTO STRICT v_election_id FROM evoting.elections
    WHERE title = 'Database Test Election';

    INSERT INTO evoting.anonymous_ballots
        (election_id, token_digest, credential, request_digest, encrypted_vote, proof, ballot_hash)
    VALUES (v_election_id, decode(repeat('01', 32), 'hex'), decode('1234', 'hex'),
        decode(repeat('02', 32), 'hex'), decode('deadbeef', 'hex'), decode('cafebabe', 'hex'), repeat('a', 64));

    BEGIN
        INSERT INTO evoting.anonymous_ballots
            (election_id, token_digest, credential, request_digest, encrypted_vote, proof, ballot_hash)
        SELECT election_id, token_digest, credential, request_digest, encrypted_vote, proof, repeat('b', 64)
        FROM evoting.anonymous_ballots WHERE election_id = v_election_id;
        RAISE EXCEPTION 'Duplicate voting credential was accepted';
    EXCEPTION WHEN unique_violation THEN
        RAISE NOTICE 'PASS: duplicate voting credential rejected.';
    END;

    BEGIN
        INSERT INTO evoting.anonymous_ballots
            (election_id, token_digest, credential, request_digest, encrypted_vote, proof, ballot_hash)
        SELECT election_id, decode(repeat('03', 32), 'hex'), credential, request_digest, encrypted_vote, proof, ballot_hash
        FROM evoting.anonymous_ballots WHERE election_id = v_election_id;
        RAISE EXCEPTION 'Duplicate ballot hash was accepted';
    EXCEPTION WHEN unique_violation THEN
        RAISE NOTICE 'PASS: duplicate ballot hash rejected.';
    END;

    IF to_regclass('evoting.receipts') IS NOT NULL OR to_regclass('evoting.encrypted_ballots') IS NOT NULL OR EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'evoting' AND table_name = 'anonymous_ballots'
            AND column_name IN ('user_id', 'receipt_id')
    ) THEN
        RAISE EXCEPTION 'Legacy ballot storage or identity links still exist';
    END IF;
END $$;

-- ------------------------------------------------------------
-- 10. Foreign-key integrity
-- ------------------------------------------------------------
\echo ''
\echo '=== 10. FOREIGN KEY INTEGRITY TEST ==='

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
\echo '=== 11. TEST DATA SUMMARY ==='

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
       FROM evoting.anonymous_ballots AS ab
       JOIN evoting.elections AS e ON e.id = ab.election_id
      WHERE e.title = 'Database Test Election') AS test_ballots;

\echo ''
\echo '============================================================'
\echo ' ALL TESTS PASSED'
\echo '============================================================'
\echo 'Rolling back test data...'

ROLLBACK;

\echo 'Test data removed.'

