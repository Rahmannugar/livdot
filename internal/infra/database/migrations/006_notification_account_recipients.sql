ALTER TABLE email_notifications
    DROP CONSTRAINT email_notifications_recipient_user_id_fkey;

ALTER TABLE email_notifications
    RENAME COLUMN recipient_user_id TO recipient_account_id;

ALTER TABLE email_notifications
    ADD CONSTRAINT email_notifications_recipient_account_id_fkey
    FOREIGN KEY (recipient_account_id)
    REFERENCES authentication_accounts (id)
    ON DELETE SET NULL;

---- create above / drop below ----

ALTER TABLE email_notifications
    DROP CONSTRAINT email_notifications_recipient_account_id_fkey;

-- Crew notifications cannot reference users, so make them anonymous before
-- restoring the narrower historical foreign key.
UPDATE email_notifications AS notification
SET recipient_account_id = NULL
WHERE recipient_account_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM users
      WHERE users.account_id = notification.recipient_account_id
  );

ALTER TABLE email_notifications
    RENAME COLUMN recipient_account_id TO recipient_user_id;

ALTER TABLE email_notifications
    ADD CONSTRAINT email_notifications_recipient_user_id_fkey
    FOREIGN KEY (recipient_user_id)
    REFERENCES users (account_id)
    ON DELETE SET NULL;
