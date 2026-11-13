BEGIN;

ALTER TABLE evoting.users DROP COLUMN IF EXISTS created_at;
ALTER TABLE evoting.elections DROP COLUMN IF EXISTS created_at;
ALTER TABLE evoting.election_keys DROP COLUMN IF EXISTS created_at;

COMMIT;
