-- CHRN-132: the applied-migrations rows, compared separately from the shape
-- because the asr database keeps its own ledger under another name.
SELECT 'mig|' || version FROM public.schema_migrations ORDER BY version COLLATE "C";
