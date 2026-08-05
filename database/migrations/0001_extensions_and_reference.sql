BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE countries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  iso2 char(2) NOT NULL UNIQUE,
  iso3 char(3) NOT NULL UNIQUE,
  name text NOT NULL,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE administrative_areas (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  country_id uuid NOT NULL REFERENCES countries(id),
  parent_id uuid REFERENCES administrative_areas(id),
  level smallint NOT NULL CHECK (level BETWEEN 1 AND 4),
  code text,
  name text NOT NULL,
  area_type text NOT NULL,
  active boolean NOT NULL DEFAULT true,
  valid_from date,
  valid_to date,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE NULLS NOT DISTINCT (country_id, parent_id, level, name)
);

CREATE INDEX idx_administrative_areas_parent ON administrative_areas(parent_id, active);
CREATE INDEX idx_administrative_areas_country_level ON administrative_areas(country_id, level, active);

CREATE TABLE gender_options (
  code text PRIMARY KEY,
  display_name text NOT NULL,
  display_order integer NOT NULL DEFAULT 100,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO gender_options(code, display_name, display_order) VALUES
  ('FEMALE', 'Female', 10),
  ('MALE', 'Male', 20),
  ('OTHER', 'Other', 30),
  ('PREFER_NOT_TO_SAY', 'Prefer not to say', 40),
  ('NOT_STATED', 'Not stated', 50)
ON CONFLICT DO NOTHING;

COMMIT;
