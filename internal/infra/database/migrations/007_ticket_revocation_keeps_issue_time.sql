ALTER TABLE tickets DROP CONSTRAINT tickets_issued_at_consistent;

-- A revoked ticket was issued first, so it keeps issued_at. Only tickets that
-- never left the reservation phase must have a null issued_at.
ALTER TABLE tickets ADD CONSTRAINT tickets_issued_at_consistent CHECK (
    (status IN ('temporarily_reserved', 'reservation_expired') AND issued_at IS NULL)
    OR (status = 'issued' AND issued_at IS NOT NULL)
    OR (status = 'revoked')
);

---- create above / drop below ----

ALTER TABLE tickets DROP CONSTRAINT tickets_issued_at_consistent;

ALTER TABLE tickets ADD CONSTRAINT tickets_issued_at_consistent CHECK (
    (status = 'issued' AND issued_at IS NOT NULL)
    OR (status <> 'issued' AND issued_at IS NULL)
);
