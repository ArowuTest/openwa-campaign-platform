BEGIN;

ALTER TABLE inbound_replies
  DROP CONSTRAINT IF EXISTS inbound_replies_message_text_check;

ALTER TABLE inbound_replies
  ADD CONSTRAINT inbound_replies_message_text_check CHECK (
    (message_text <> '' AND length(message_text) BETWEEN 1 AND 4096)
    OR (
      message_text = ''
      AND (message_text_cipher IS NOT NULL OR content_redacted_at IS NOT NULL)
    )
  );

COMMIT;
