--
-- OAuth (Google OpenID Connect) identities.
--
-- A row links one external identity (provider + the provider's immutable "sub"
-- claim) to one evoting account. The provider email is stored only as a
-- snapshot for display and auditing; it is never used to find the account at
-- login time, because Google lets users change the email on an account.
--

CREATE TABLE IF NOT EXISTS evoting.oauth_identities (
    id            uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id       uuid NOT NULL,
    provider      character varying(32) NOT NULL,
    subject       character varying(255) NOT NULL,
    email         character varying(255) NOT NULL,
    created_at    timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    last_login_at timestamp with time zone
);

ALTER TABLE evoting.oauth_identities OWNER TO evoting_app;

ALTER TABLE ONLY evoting.oauth_identities
    ADD CONSTRAINT oauth_identities_pkey PRIMARY KEY (id);

-- One external identity can only ever map to one app account.
ALTER TABLE ONLY evoting.oauth_identities
    ADD CONSTRAINT unique_provider_subject UNIQUE (provider, subject);

-- One app account can only be linked to a given provider once.
ALTER TABLE ONLY evoting.oauth_identities
    ADD CONSTRAINT unique_user_provider UNIQUE (user_id, provider);

ALTER TABLE ONLY evoting.oauth_identities
    ADD CONSTRAINT fk_oauth_identity_user
    FOREIGN KEY (user_id) REFERENCES evoting.users(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS oauth_identities_user_id_idx
    ON evoting.oauth_identities (user_id);

DROP INDEX IF EXISTS evoting.oauth_identities_provider_subject_idx;
