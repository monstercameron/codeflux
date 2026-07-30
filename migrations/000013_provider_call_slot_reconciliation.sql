-- Reconcile conservative provider retry reservations with physical attempts.

ALTER TABLE budget_usage_postings
ADD COLUMN actual_provider_call_slots INTEGER NOT NULL DEFAULT 0 CHECK (
    actual_provider_call_slots BETWEEN 0 AND 4294967295
);

ALTER TABLE budget_reconciliation_intents
ADD COLUMN actual_provider_call_slots INTEGER NOT NULL DEFAULT 0 CHECK (
    actual_provider_call_slots BETWEEN 0 AND 4294967295
);
