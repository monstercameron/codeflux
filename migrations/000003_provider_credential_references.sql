-- Provider rows retain only opaque operating-system credential identities.

CREATE TABLE provider_credential_references (
    provider_id TEXT PRIMARY KEY REFERENCES providers(id) ON DELETE CASCADE,
    opaque_reference TEXT NOT NULL UNIQUE CHECK (
        length(opaque_reference) BETWEEN 8 AND 512
        AND opaque_reference LIKE 'os://%/%'
        AND instr(opaque_reference, char(0)) = 0
        AND instr(opaque_reference, char(10)) = 0
        AND instr(opaque_reference, char(13)) = 0
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    updated_at_unix_micros INTEGER NOT NULL CHECK (
        updated_at_unix_micros >= created_at_unix_micros
    ),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0)
) STRICT;

CREATE INDEX provider_credential_references_lookup
    ON provider_credential_references(opaque_reference);
