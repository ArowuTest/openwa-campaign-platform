BEGIN;
ALTER TABLE inbound_replies
 ADD COLUMN IF NOT EXISTS message_text_cipher bytea,
 ADD COLUMN IF NOT EXISTS content_key_version text;
COMMENT ON COLUMN inbound_replies.message_text IS 'Legacy plaintext column; new writes use message_text_cipher and retention redaction clears both.';
COMMENT ON COLUMN inbound_replies.message_text_cipher IS 'Purpose-bound application-encrypted inbound reply content.';
COMMIT;
