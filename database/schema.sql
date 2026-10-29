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
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    he_public_key bytea,
    dsa_public_key bytea,
    kem_public_key bytea
);


ALTER TABLE evoting.elections OWNER TO evoting_app;

--
-- Name: election_secrets; Type: TABLE; Schema: evoting; Owner: evoting_app
-- The *_enc columns must hold ciphertext (encrypted at rest by the security layer).
--

CREATE TABLE evoting.election_secrets (
    election_id uuid NOT NULL,
    he_secret_key_enc bytea NOT NULL,
    dsa_private_key_enc bytea NOT NULL,
    kem_private_key_enc bytea NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


ALTER TABLE evoting.election_secrets OWNER TO evoting_app;

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
-- Name: encrypted_ballots; Type: TABLE; Schema: evoting; Owner: evoting_app
--

CREATE TABLE evoting.encrypted_ballots (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    election_id uuid NOT NULL,
    receipt_id uuid,
    encrypted_vote bytea NOT NULL,
    proof bytea,
    ballot_hash character varying NOT NULL
);


ALTER TABLE evoting.encrypted_ballots OWNER TO evoting_app;

--
-- Name: receipts; Type: TABLE; Schema: evoting; Owner: evoting_app
--

CREATE TABLE evoting.receipts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    election_id uuid NOT NULL,
    receipt_code character varying NOT NULL
);


ALTER TABLE evoting.receipts OWNER TO evoting_app;

--
-- Name: users; Type: TABLE; Schema: evoting; Owner: evoting_app
--

CREATE TABLE evoting.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email character varying(255) NOT NULL,
    password_hash character varying(255) NOT NULL,
    role evoting.roles NOT NULL,
    is_verified boolean DEFAULT false NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


ALTER TABLE evoting.users OWNER TO evoting_app;

--
-- Name: voter_eligibility; Type: TABLE; Schema: evoting; Owner: evoting_app
--

CREATE TABLE evoting.voter_eligibility (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    election_id uuid NOT NULL,
    user_id uuid NOT NULL,
    receipt_id uuid,
    has_voted boolean DEFAULT false NOT NULL
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
-- Name: encrypted_ballots encrypted_ballots_ballot_hash_key; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.encrypted_ballots
    ADD CONSTRAINT encrypted_ballots_ballot_hash_key UNIQUE (ballot_hash);


--
-- Name: encrypted_ballots encrypted_ballots_pkey; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.encrypted_ballots
    ADD CONSTRAINT encrypted_ballots_pkey PRIMARY KEY (id);

ALTER TABLE ONLY evoting.encrypted_ballots
    ADD CONSTRAINT encrypted_ballots_receipt_id_key UNIQUE (receipt_id);


--
-- Name: receipts receipts_pkey; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.receipts
    ADD CONSTRAINT receipts_pkey PRIMARY KEY (id);


--
-- Name: receipts receipts_receipt_code_key; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.receipts
    ADD CONSTRAINT receipts_receipt_code_key UNIQUE (receipt_code);


--
-- Name: election_secrets election_secrets_pkey; Type: CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.election_secrets
    ADD CONSTRAINT election_secrets_pkey PRIMARY KEY (election_id);


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

ALTER TABLE ONLY evoting.voter_eligibility
    ADD CONSTRAINT voter_eligibility_receipt_id_key UNIQUE (receipt_id);


--
-- Name: encrypted_ballots fk_ballot_election; Type: FK CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.encrypted_ballots
    ADD CONSTRAINT fk_ballot_election FOREIGN KEY (election_id) REFERENCES evoting.elections(id);

ALTER TABLE ONLY evoting.encrypted_ballots
    ADD CONSTRAINT fk_ballot_receipt FOREIGN KEY (receipt_id) REFERENCES evoting.receipts(id);


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

ALTER TABLE ONLY evoting.voter_eligibility
    ADD CONSTRAINT fk_eligibility_receipt FOREIGN KEY (receipt_id) REFERENCES evoting.receipts(id);


--
-- Name: election_secrets fk_secrets_election; Type: FK CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.election_secrets
    ADD CONSTRAINT fk_secrets_election FOREIGN KEY (election_id) REFERENCES evoting.elections(id) ON DELETE CASCADE;


--
-- Name: receipts fk_receipt_election; Type: FK CONSTRAINT; Schema: evoting; Owner: evoting_app
--

ALTER TABLE ONLY evoting.receipts
    ADD CONSTRAINT fk_receipt_election FOREIGN KEY (election_id) REFERENCES evoting.elections(id);


--
-- PostgreSQL database dump complete
--

\unrestrict VhlNnTXQkm1Ne6HLlI4l2rixlaeobNxCDpFyCmbXk5ecUOf0pr1uTFeACcz1jlv
