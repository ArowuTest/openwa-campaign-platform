BEGIN;

ALTER TABLE attribute_definitions
  ADD COLUMN IF NOT EXISTS core boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS storage text NOT NULL DEFAULT 'contact_attribute',
  ADD COLUMN IF NOT EXISTS queryable_field text,
  ADD COLUMN IF NOT EXISTS attribute_value_column text,
  ADD COLUMN IF NOT EXISTS allowed_values jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE attribute_definitions DROP CONSTRAINT IF EXISTS attribute_definitions_storage_check;
ALTER TABLE attribute_definitions ADD CONSTRAINT attribute_definitions_storage_check
  CHECK (storage IN ('core_column','contact_attribute'));
ALTER TABLE attribute_definitions DROP CONSTRAINT IF EXISTS attribute_definitions_attribute_column_check;
ALTER TABLE attribute_definitions ADD CONSTRAINT attribute_definitions_attribute_column_check CHECK (
  (storage='core_column' AND queryable_field IS NOT NULL AND attribute_value_column IS NULL)
  OR
  (storage='contact_attribute' AND core=false AND queryable_field IS NULL AND attribute_value_column IN ('value_text','value_integer','value_decimal','value_boolean','value_date'))
);
ALTER TABLE attribute_definitions DROP CONSTRAINT IF EXISTS attribute_definitions_allowed_values_check;
ALTER TABLE attribute_definitions ADD CONSTRAINT attribute_definitions_allowed_values_check CHECK (jsonb_typeof(allowed_values)='array');

INSERT INTO attribute_definitions(code,display_name,description,data_type,allowed_operators,filterable,reportable,sensitive,required_permission,index_strategy,display_order,active,core,storage,queryable_field,attribute_value_column,allowed_values)
VALUES
('COUNTRY','Country','Country associated with the contact profile.','geography',ARRAY['in','not_in','is_known','is_unknown'],true,true,false,NULL,'btree',10,true,true,'core_column','c.country_id',NULL,'[]'),
('STATE','State or region','State, province or first-level administrative region.','geography',ARRAY['in','not_in','is_known','is_unknown'],true,true,false,NULL,'btree',20,true,true,'core_column','c.state_id',NULL,'[]'),
('LGA','LGA or district','Local government area, district or second-level administrative region.','geography',ARRAY['in','not_in','is_known','is_unknown'],true,true,false,NULL,'btree',30,true,true,'core_column','c.lga_id',NULL,'[]'),
('REPORTED_AGE','Reported age','Age supplied by the contact when the source record was collected.','integer',ARRAY['equals','between','greater_than','less_than','is_known','is_unknown'],true,true,true,'audience.demographics.read','btree',40,true,true,'core_column','c.reported_age',NULL,'[]'),
('AGE_RECORDED_AT','Age recorded date','Date on which the self-declared age was collected.','date',ARRAY['equals','between','greater_than','less_than','is_known','is_unknown'],true,true,true,'audience.demographics.read','btree',45,true,true,'core_column','c.age_recorded_at',NULL,'[]'),
('GENDER','Gender','Optional self-declared gender value.','single_select',ARRAY['in','not_in','is_known','is_unknown'],true,true,true,'audience.demographics.read','btree',50,true,true,'core_column','c.gender_code',NULL,'[]'),
('CONSENT_PURPOSE','Consent purpose','Approved marketing purpose covered by active consent.','multi_select',ARRAY['in','not_in'],true,true,false,NULL,'composite-partial',60,true,true,'core_column','cg.purpose_id',NULL,'[]'),
('PREFERRED_LANGUAGE','Preferred language','Preferred language supplied with the contact record.','single_select',ARRAY['in','not_in','is_known','is_unknown'],true,true,false,NULL,'btree',70,true,true,'core_column','c.preferred_language_code',NULL,'[]')
ON CONFLICT(code) DO UPDATE SET
 display_name=EXCLUDED.display_name, description=EXCLUDED.description, data_type=EXCLUDED.data_type,
 allowed_operators=EXCLUDED.allowed_operators, filterable=EXCLUDED.filterable, reportable=EXCLUDED.reportable,
 sensitive=EXCLUDED.sensitive, required_permission=EXCLUDED.required_permission, index_strategy=EXCLUDED.index_strategy,
 display_order=EXCLUDED.display_order, active=true, core=true, storage='core_column',
 queryable_field=EXCLUDED.queryable_field, attribute_value_column=NULL, updated_at=now();

CREATE TABLE IF NOT EXISTS filter_definition_change_history(
  id bigserial PRIMARY KEY,
  definition_id uuid NOT NULL REFERENCES attribute_definitions(id) ON DELETE RESTRICT,
  definition_version integer NOT NULL,
  actor_id text NOT NULL,
  reason text NOT NULL,
  snapshot jsonb NOT NULL,
  changed_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(definition_id,definition_version)
);
CREATE INDEX IF NOT EXISTS idx_filter_definition_history ON filter_definition_change_history(definition_id,definition_version DESC);

CREATE TABLE IF NOT EXISTS filter_registry_generation(
  singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
  generation bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO filter_registry_generation(singleton,generation) VALUES(true,1) ON CONFLICT(singleton) DO NOTHING;

CREATE OR REPLACE FUNCTION filter_definition_governance() RETURNS trigger AS $$
BEGIN
  IF TG_OP='UPDATE' THEN
    IF OLD.code<>NEW.code OR OLD.data_type<>NEW.data_type OR OLD.core<>NEW.core OR OLD.storage<>NEW.storage
       OR OLD.queryable_field IS DISTINCT FROM NEW.queryable_field
       OR OLD.attribute_value_column IS DISTINCT FROM NEW.attribute_value_column THEN
      RAISE EXCEPTION 'filter identity/storage fields are immutable' USING ERRCODE='55000';
    END IF;
    IF OLD.code IN ('COUNTRY','STATE','LGA','REPORTED_AGE','GENDER') AND (NEW.active=false OR NEW.filterable=false) THEN
      RAISE EXCEPTION 'mandatory filter cannot be disabled' USING ERRCODE='55000';
    END IF;
    NEW.version=OLD.version+1;
    NEW.updated_at=now();
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_filter_definition_governance ON attribute_definitions;
CREATE TRIGGER trg_filter_definition_governance BEFORE UPDATE ON attribute_definitions FOR EACH ROW EXECUTE FUNCTION filter_definition_governance();

CREATE OR REPLACE FUNCTION increment_filter_registry_generation() RETURNS trigger AS $$
BEGIN
  UPDATE filter_registry_generation SET generation=generation+1,updated_at=now() WHERE singleton=true;
  RETURN COALESCE(NEW,OLD);
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_filter_registry_generation ON attribute_definitions;
CREATE TRIGGER trg_filter_registry_generation AFTER INSERT OR UPDATE OR DELETE ON attribute_definitions FOR EACH STATEMENT EXECUTE FUNCTION increment_filter_registry_generation();

CREATE OR REPLACE FUNCTION prevent_filter_history_mutation() RETURNS trigger AS $$
BEGIN
 RAISE EXCEPTION 'filter definition history is append-only' USING ERRCODE='55000';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_filter_history_immutable ON filter_definition_change_history;
CREATE TRIGGER trg_filter_history_immutable BEFORE UPDATE OR DELETE ON filter_definition_change_history FOR EACH ROW EXECUTE FUNCTION prevent_filter_history_mutation();

COMMIT;
