--
-- PostgreSQL database dump
--

\restrict VhlNnTXQkm1Ne6HLlI4l2rixlaeobNxCDpFyCmbXk5ecUOf0pr1uTFeACcz1jlv

-- Dumped from database version 18.6 (Debian 18.6-3)
-- Dumped by pg_dump version 18.6 (Debian 18.6-3)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: evoting; Type: SCHEMA; Schema: -; Owner: evoting_app
--

CREATE SCHEMA evoting;


ALTER SCHEMA evoting OWNER TO evoting_app;

--
-- Name: roles; Type: TYPE; Schema: evoting; Owner: evoting_app
--

CREATE TYPE evoting.roles AS ENUM (
    'admin',
    'voter'
);


ALTER TYPE evoting.roles OWNER TO evoting_app;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: candidates; Type: TABLE; Schema: evoting; Owner: evoting_app
--

CREATE TABLE evoting.candidates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    election_id uuid NOT NULL,
    name character varying NOT NULL,
    description text,
    position integer NOT NULL
);


ALTER TABLE evoting.candidates OWNER TO evoting_app;

--
-- Name: elections; Type: TABLE; Schema: evoting; Owner: evoting_app
--

CREATE TABLE evoting.elections (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    title character varying NOT NULL,
    description text,
    registration_start timestamp with time zone NOT NULL,
    registration_end timestamp with time zone NOT NULL,
    voting_start timestamp with time zone NOT NULL,
    voting_end timestamp with time zone NOT NULL,
    status character varying NOT NULL,
    created_by uuid NOT NULL
);


ALTER TABLE evoting.elections OWNER TO evoting_app;

--
-- Name: assign_candidate_position(); Type: FUNCTION; Schema: evoting; Owner: evoting_app
--

CREATE FUNCTION evoting.assign_candidate_position() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.position IS NULL THEN
        SELECT COALESCE(MAX(position) + 1, 0)
        INTO NEW.position
        FROM evoting.candidates
        WHERE election_id = NEW.election_id;
    END IF;
    RETURN NEW;
END;
$$;


ALTER FUNCTION evoting.assign_candidate_position() OWNER TO evoting_app;

--
-- Name: users; Type: TABLE; Schema: evoting; Owner: evoting_app
--

CREATE TABLE evoting.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email character varying(255) NOT NULL,
    password_hash character varying(255) NOT NULL,
    role evoting.roles NOT NULL,
    is_verified boolean DEFAULT false NOT NULL
);


ALTER TABLE evoting.users OWNER TO evoting_app;

--
-- Name: voter_eligibility; Type: TABLE; Schema: evoting; Owner: evoting_app
--

CREATE TABLE evoting.voter_eligibility (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    election_id uuid NOT NULL,
    user_id uuid NOT NULL
);


ALTER TABLE evoting.voter_eligibility OWNER TO evoting_app;

--
-- Name: candidates candidates_pkey; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.candidates
    ADD CONSTRAINT candidates_pkey PRIMARY KEY (id);


--
-- Name: elections elections_pkey; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.elections
    ADD CONSTRAINT elections_pkey PRIMARY KEY (id);


--
-- Name: candidates unique_candidate_position; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.candidates
    ADD CONSTRAINT unique_candidate_position UNIQUE (election_id, position);


--
-- Name: candidates candidates_assign_position; Type: TRIGGER; Schema: evoting; Owner: evoting_app
--

CREATE TRIGGER candidates_assign_position BEFORE INSERT ON evoting.candidates FOR EACH ROW EXECUTE FUNCTION evoting.assign_candidate_position();


--
-- Name: candidates unique_candidate_name; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.candidates
    ADD CONSTRAINT unique_candidate_name UNIQUE (election_id, name);


--
-- Name: voter_eligibility unique_voter_election; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.voter_eligibility
    ADD CONSTRAINT unique_voter_election UNIQUE (election_id, user_id);


--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.users
    ADD CONSTRAINT users_email_key UNIQUE (email);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: voter_eligibility voter_eligibility_pkey; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.voter_eligibility
    ADD CONSTRAINT voter_eligibility_pkey PRIMARY KEY (id);



--
-- Name: candidates fk_candidate_election; Type: FK CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.candidates
    ADD CONSTRAINT fk_candidate_election FOREIGN KEY (election_id) REFERENCES evoting.elections(id);


--
-- Name: elections fk_election_creator; Type: FK CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.elections
    ADD CONSTRAINT fk_election_creator FOREIGN KEY (created_by) REFERENCES evoting.users(id);


--
-- Name: voter_eligibility fk_eligibility_election; Type: FK CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.voter_eligibility
    ADD CONSTRAINT fk_eligibility_election FOREIGN KEY (election_id) REFERENCES evoting.elections(id);


--
-- Name: voter_eligibility fk_eligibility_user; Type: FK CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.voter_eligibility
    ADD CONSTRAINT fk_eligibility_user FOREIGN KEY (user_id) REFERENCES evoting.users(id);




CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;

CREATE TABLE IF NOT EXISTS evoting.election_keys (
    election_id uuid PRIMARY KEY REFERENCES evoting.elections(id) ON DELETE CASCADE,
    kem_public_key bytea NOT NULL,
    kem_private_key bytea NOT NULL,
    dsa_public_key bytea NOT NULL,
    dsa_private_key bytea NOT NULL,
    he_public_key bytea NOT NULL,
    he_secret_key_fingerprint character varying(64) NOT NULL
);

ALTER TABLE evoting.election_keys OWNER TO evoting_app;

CREATE TABLE IF NOT EXISTS evoting.election_results (
    election_id uuid PRIMARY KEY REFERENCES evoting.elections(id) ON DELETE CASCADE,
    total_ballots integer NOT NULL DEFAULT 0,
    results jsonb NOT NULL DEFAULT '[]'::jsonb,
    tallied_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER TABLE evoting.election_results OWNER TO evoting_app;

BEGIN;

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
    credential bytea NOT NULL,
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    encrypted_vote bytea NOT NULL,
    proof bytea NOT NULL,
    ballot_hash text NOT NULL UNIQUE CHECK (ballot_hash ~ '^[0-9a-f]{64}$'),
    PRIMARY KEY (election_id, token_digest)
);

CREATE TABLE IF NOT EXISTS evoting.anonymous_issuer_keys (
    election_id uuid PRIMARY KEY REFERENCES evoting.elections(id) ON DELETE CASCADE,
    public_key bytea NOT NULL,
    private_key_enc bytea NOT NULL
);

COMMIT;

--
-- PostgreSQL database dump complete
--

\unrestrict VhlNnTXQkm1Ne6HLlI4l2rixlaeobNxCDpFyCmbXk5ecUOf0pr1uTFeACcz1jlv
