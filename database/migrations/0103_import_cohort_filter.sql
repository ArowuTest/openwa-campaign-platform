BEGIN;

-- Reserved built-in: fail on a conflicting pre-existing custom definition.
INSERT INTO attribute_definitions(code, display_name, description, data_type,
  allowed_operators, filterable, reportable, sensitive, index_strategy, display_order,
  active, core, storage, queryable_field)
VALUES ('IMPORT_COHORT', 'Upload cohort', 'One stable group within a completed, consent-reviewed upload.',
  'text', ARRAY['equals'], true, false, false, 'btree', 80, true, true, 'core_column', 'c.id')
ON CONFLICT (code) DO NOTHING;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM attribute_definitions WHERE code='IMPORT_COHORT'
    AND core AND storage='core_column' AND queryable_field='c.id' AND data_type='text'
    AND allowed_operators=ARRAY['equals']) THEN
    RAISE EXCEPTION 'IMPORT_COHORT conflicts with an existing filter definition';
  END IF;
END;
$$;

COMMIT;
