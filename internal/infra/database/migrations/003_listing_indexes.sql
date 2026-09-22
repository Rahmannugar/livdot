CREATE INDEX events_starts_id_idx ON events (starts_at, id);

-- Supports case-insensitive crew name listing ordered by (crew_name, account_id).
CREATE INDEX crews_name_account_idx ON crews (crew_name, account_id);

---- create above / drop below ----

DROP INDEX crews_name_account_idx;
DROP INDEX events_starts_id_idx;
