BEGIN;

INSERT INTO countries(iso2, iso3, name)
VALUES
  ('NG', 'NGA', 'Nigeria'),
  ('GH', 'GHA', 'Ghana'),
  ('GB', 'GBR', 'United Kingdom')
ON CONFLICT (iso2) DO UPDATE SET name = EXCLUDED.name, active = true;

WITH nigeria AS (
  SELECT id FROM countries WHERE iso2 = 'NG'
)
INSERT INTO administrative_areas(country_id, parent_id, level, code, name, area_type)
SELECT nigeria.id, NULL, 1, state.code, state.name, 'STATE'
FROM nigeria
CROSS JOIN (VALUES
  ('ABIA','Abia'),('ADAMAWA','Adamawa'),('AKWA_IBOM','Akwa Ibom'),('ANAMBRA','Anambra'),
  ('BAUCHI','Bauchi'),('BAYELSA','Bayelsa'),('BENUE','Benue'),('BORNO','Borno'),
  ('CROSS_RIVER','Cross River'),('DELTA','Delta'),('EBONYI','Ebonyi'),('EDO','Edo'),
  ('EKITI','Ekiti'),('ENUGU','Enugu'),('FCT','Federal Capital Territory'),('GOMBE','Gombe'),
  ('IMO','Imo'),('JIGAWA','Jigawa'),('KADUNA','Kaduna'),('KANO','Kano'),('KATSINA','Katsina'),
  ('KEBBI','Kebbi'),('KOGI','Kogi'),('KWARA','Kwara'),('LAGOS','Lagos'),('NASARAWA','Nasarawa'),
  ('NIGER','Niger'),('OGUN','Ogun'),('ONDO','Ondo'),('OSUN','Osun'),('OYO','Oyo'),
  ('PLATEAU','Plateau'),('RIVERS','Rivers'),('SOKOTO','Sokoto'),('TARABA','Taraba'),
  ('YOBE','Yobe'),('ZAMFARA','Zamfara')
) AS state(code, name)
ON CONFLICT (country_id, parent_id, level, name)
DO UPDATE SET code = EXCLUDED.code, active = true;

WITH nigeria AS (
  SELECT id FROM countries WHERE iso2 = 'NG'
), lagos AS (
  SELECT a.id, a.country_id
  FROM administrative_areas a
  JOIN nigeria n ON n.id = a.country_id
  WHERE a.level = 1 AND a.code = 'LAGOS'
)
INSERT INTO administrative_areas(country_id, parent_id, level, code, name, area_type)
SELECT lagos.country_id, lagos.id, 2, lga.code, lga.name, 'LGA'
FROM lagos
CROSS JOIN (VALUES
  ('AGEGE','Agege'),('AJEROMI_IFELODUN','Ajeromi-Ifelodun'),('ALIMOSHO','Alimosho'),
  ('AMUWO_ODOFIN','Amuwo-Odofin'),('APAPA','Apapa'),('BADAGRY','Badagry'),('EPE','Epe'),
  ('ETI_OSA','Eti-Osa'),('IBEJU_LEKKI','Ibeju-Lekki'),('IFAKO_IJAIYE','Ifako-Ijaiye'),
  ('IKEJA','Ikeja'),('IKORODU','Ikorodu'),('KOSOFE','Kosofe'),('LAGOS_ISLAND','Lagos Island'),
  ('LAGOS_MAINLAND','Lagos Mainland'),('MUSHIN','Mushin'),('OJO','Ojo'),
  ('OSHODI_ISOLO','Oshodi-Isolo'),('SHOMOLU','Shomolu'),('SURULERE','Surulere')
) AS lga(code, name)
ON CONFLICT (country_id, parent_id, level, name)
DO UPDATE SET code = EXCLUDED.code, active = true;

COMMIT;
