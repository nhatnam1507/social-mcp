-- The OAuth app (client ID) that issued the login; '' for logins made before it was recorded.
ALTER TABLE credentials ADD COLUMN client_id TEXT NOT NULL DEFAULT '';
