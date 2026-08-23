-- Reverses 0001_init. CASCADE because a down migration that fails on a
-- dependent object is a down migration nobody can run.
DROP SCHEMA IF EXISTS tier1 CASCADE;
DROP SCHEMA IF EXISTS tier2 CASCADE;
