BEGIN;

-- Govern message-media rights evidence without fabricating legacy authority.

ALTER TABLE trusted_assets
  ADD COLUMN IF NOT EXISTS rights_basis text,
  ADD COLUMN IF NOT EXISTS rights_reference text,
  ADD COLUMN IF NOT EXISTS rights_expires_at timestamptz;

ALTER TABLE trusted_assets
  DROP CONSTRAINT IF EXISTS trusted_assets_rights_shape_check;
ALTER TABLE trusted_assets
  ADD CONSTRAINT trusted_assets_rights_shape_check CHECK (
    (
      rights_basis IS NULL
      AND rights_reference IS NULL
      AND rights_expires_at IS NULL
    )
    OR
    (
      purpose='MESSAGE_MEDIA'
      AND rights_basis IN ('OWNED','LICENSED','CLIENT_SUPPLIED','PUBLIC_DOMAIN')
      AND rights_reference IS NOT NULL
      AND length(btrim(rights_reference)) BETWEEN 3 AND 500
      AND rights_reference !~ E'[\r\n]'
    )
  );

-- Rights are immutable evidence. The same media bytes may legitimately be
-- governed again under a renewed or different rights instrument.
ALTER TABLE trusted_assets
  DROP CONSTRAINT IF EXISTS trusted_assets_purpose_sha256_checksum_byte_size_key;
CREATE INDEX IF NOT EXISTS trusted_assets_content_lookup_idx
  ON trusted_assets(purpose,sha256_checksum,byte_size);
CREATE INDEX IF NOT EXISTS trusted_assets_message_rights_expiry_idx
  ON trusted_assets(rights_expires_at,id)
  WHERE purpose='MESSAGE_MEDIA' AND status='CLEAN' AND rights_expires_at IS NOT NULL;

COMMIT;
