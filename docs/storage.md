# Codeflux SQLite Storage Decisions

This document records the concrete storage choices that implement the authority
and recovery rules in [the Codeflux plan](plan.md#23-storage).

## Driver

Codeflux uses `modernc.org/sqlite` v1.55.0 through `database/sql`.

- The driver is a CGO-free port of SQLite. Builds do not require a C compiler,
  a platform SQLite library, or the SQLite CLI.
- Its supported platform matrix includes the declared Windows ARM64 and AMD64,
  macOS ARM64, and Linux AMD64 CI targets.
- Codeflux pins the driver and its transitive `modernc.org/libc` dependency
  through `go.mod` and `go.sum`; dependency upgrades are reviewed together.

Primary references:

- <https://pkg.go.dev/modernc.org/sqlite>
- <https://gitlab.com/cznic/sqlite>

## Location and permissions

The default database is `codeflux.sqlite3` in the operating-system application
data directory:

- Windows: `%LOCALAPPDATA%\Codeflux`
- macOS: `~/Library/Application Support/Codeflux`
- Linux: `$XDG_DATA_HOME/codeflux`, or `~/.local/share/codeflux`

The directory is created with mode `0700` and the database with mode `0600`
where the operating system enforces POSIX permission bits. Windows access
control remains the responsibility of the user-local application-data
directory until the credential and installer milestone adds native ACL work.

## Connection and durability policy

Every connection enables foreign keys, WAL journaling, a bounded busy timeout,
`synchronous=FULL`, and disabled double-quoted string compatibility.

`FULL` is intentional: Codeflux considers committed task events and authority
decisions durable state. SQLite documents `FULL` in WAL mode as durable across
application, operating-system, and power failures. A future performance change
requires durability evidence and a plan amendment; it is not a user speed
preset.

The default pool permits four open and four idle connections. SQLite still has
one writer, while WAL permits bounded concurrent readers. Mutating transaction
runners acquire write intent immediately so contention fails or waits at the
transaction boundary rather than after application reads.

Primary references:

- <https://www.sqlite.org/pragma.html#pragma_foreign_keys>
- <https://www.sqlite.org/pragma.html#pragma_synchronous>
- <https://www.sqlite.org/wal.html>
- <https://www.sqlite.org/lang_transaction.html>

## Migration and recovery policy

Migrations are immutable, embedded, checksum-bound, and monotonically numbered.
The database records the application version, checksum, start, completion,
result, failure text, and recovery snapshot for every attempt.

Before pending migrations run, Codeflux:

1. acquires an OS-backed cross-process lock beside the database;
2. refuses schemas newer than the binary or histories whose checksums differ;
3. verifies free space for two database sizes plus a fixed safety reserve;
4. creates a restrictive snapshot with SQLite's Online Backup API; and
5. executes each migration and version/history update in one transaction.

A failed migration is rolled back, the snapshot is restored, and one stable
failure record is written. Later startups report that record without retrying
or changing the database. If a process stops after recording a migration start,
the next startup restores the recorded snapshot, marks the attempt failed, and
also refuses an automatic retry.

Primary reference:

- <https://www.sqlite.org/backup.html>

## Initial schema mutability

Migration `000001_initial_operational_schema.sql` creates the Phase 1
operational entities. Mutable aggregate roots carry an optimistic integer
revision and lifecycle timestamps; repositories must compare and advance that
revision in the same transaction as each state change.

Rows that are facts rather than aggregates reject in-place updates with SQLite
triggers: messages, task events, permission decisions, pricing snapshots,
forecasts, usage records, redacted output chunks, and diff summaries. New facts
append new rows. Deletion is deliberately not trigger-blocked here because the
later explicit deletion lifecycle must be able to satisfy user erasure without
manual database mutation.

## Transaction runner

Storage mutations use one immediate-write transaction runner. Application code
receives an operation-bearing transaction value, not raw SQL. The runner rolls
back on every callback error and commits only after the whole operation
succeeds.

When a context deadline is shorter than the configured SQLite busy timeout, the
runner temporarily bounds that connection's busy timeout to the remaining
deadline. It restores the configured value before releasing the connection.
This is required because the SQLite busy handler itself does not observe Go
context cancellation while waiting for a writer.

## Repository contracts

Repositories expose named domain operations rather than generic CRUD. Typed
domain IDs cross the boundary directly and are revalidated by their SQL scanner
implementations.

Project and source-repository identity are separate. Thread creation verifies
that its repository belongs to its project. Thread pages use an exclusive
`(updated_at, id)` cursor over deterministic descending order. Message append
allocates its per-thread sequence and checks its idempotency claim in the same
immediate transaction; an identical retry returns the original row and a
changed retry fails with a typed conflict.

Task transitions compare the expected state and revision, update the task, and
append the next event sequence in one transaction. The transition's
idempotency key belongs to both the event identity and deterministic transition
payload. A retry returns the committed task/event pair; a changed claim fails.

Approval requests and checkpoints use the same compare-on-retry rule. Approval
resolution and budget changes use optimistic revisions. Budget reservations and
actual postings operate only on integer minor units, verify currency, prevent
integer overflow, and reject any result above the hard cap. Validation and
evidence writes verify their task/run or task/validation lineage before commit.
