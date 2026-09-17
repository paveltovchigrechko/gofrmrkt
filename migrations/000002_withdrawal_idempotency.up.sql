ALTER TABLE withdrawals
    ADD COLUMN idempotency_key TEXT UNIQUE;