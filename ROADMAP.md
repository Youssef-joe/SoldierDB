# SoldierDB Roadmap

Where the project stands and what comes next. The milestone-by-milestone build
plan lives in [`docs/ENGINE_PLAN.md`](docs/ENGINE_PLAN.md); chapter notes are in
[`docs/roadmap.md`](docs/roadmap.md). This file tracks status and open work.

## Where we are

All nine milestones in [ENGINE_PLAN](docs/ENGINE_PLAN.md#milestones) are in
place. `go test -race ./...`, `go vet`, `make ctest`, `make rust` and
`make cpp` all pass.

- [x] **M0** Repository foundation
- [x] **M1** Pager: 4 KiB pages, dual checksummed meta pages, txid recovery
- [x] **M2** Copy-on-write B+tree: lookup, insert with split, range scan
- [x] **M3** Delete, persistent free list with page reuse, durable KV `db`.
      *Rebalancing is partial: only underfull leaves merge, and internal
      nodes never rebalance.*
- [x] **M4** `View`/`Update` and explicit `Begin`/`Commit`/`Rollback`
      (single writer, RW lock)
- [x] **M5** SQL lexer and recursive-descent parser
- [x] **M6** Catalog, typed rows, executor (filter, project, order, limit,
      aggregates, GROUP BY/HAVING, PK/UNIQUE/NOT NULL/DEFAULT, auto-rowid),
      shell
- [x] **M7** C ABI (`capi/`, handle-based, malloc'd strings)
- [x] **M8** Rust client and **M9** C++ client
- [x] Extra: `VECTOR(n)` columns with brute-force distance search
      ([docs/VECTOR.md](docs/VECTOR.md))

Known limits today: one process per file, a single writer, every query is a
full scan, no joins or subqueries, and no WAL (shadow paging only).

## Phase 1: Correctness hardening (next)

Fix the verified defects before adding features.

- [ ] Split B+tree nodes by bytes, not entry count; no panic on valid
      input; abort the pager tx on any commit failure; recover panics in
      `capi` (#2)
- [ ] Statement-level rollback (savepoints) inside explicit transactions (#3)
- [ ] Checked INTEGER arithmetic, SUM, ABS and auto-rowid (#4)
- [ ] Tolerate a torn trailing page on open; validate meta `Root`/`PageCount` (#5)
- [ ] Propagate row-decode errors from `SELECT` (#6)
- [ ] Exclusive file lock so a second handle can't lose writes (#1)
- [ ] Goroutine-safe `Engine`, or a per-session API (#7)
- [ ] Property and fuzz tests: random key/value sizes up to the maximums,
      `go test -fuzz` on the parser and row decoder
- [ ] Crash-injection test harness: kill or truncate at every write and
      fsync point, then reopen and check against the `kv.MemoryStore` oracle
- [ ] fsync the parent directory when creating a new database file

## Phase 2: Durability and storage

- [ ] Per-page checksums (detect bit rot, not just torn meta pages)
- [ ] Internal-node rebalancing and merge on delete
- [ ] Overflow pages for values larger than ~3 KB (lift `MaxValueSize`)
- [ ] File compaction / `VACUUM` (the file never shrinks today)
- [ ] Optional WAL with group commit, measured against shadow paging
- [ ] `PRAGMA`-style integrity check (tree invariants, free-list consistency)

## Phase 3: Indexes and query planning

- [ ] Secondary indexes (`CREATE INDEX`), with UNIQUE enforced by an index
      instead of a full table scan per insert (`checkUnique`)
- [ ] Range and point lookups on the PK pushed down from `WHERE`
- [ ] Simple rule-based planner and `EXPLAIN`
- [ ] Joins (nested-loop, then index-nested-loop), subqueries
- [ ] `ALTER TABLE ADD COLUMN`

## Phase 4: Concurrency and MVCC

- [ ] Read-only autocommit statements use `db.View`, so they don't take the
      write lock (see #7)
- [ ] Snapshot readers that run alongside the writer (keep freed pages
      until the oldest reader finishes)
- [ ] Multi-process access via file locking or a lock page (follows #1)
- [ ] MVCC with conflict detection for concurrent writers

## Phase 5: Vector search

- [ ] ANN index (HNSW or IVF) behind the existing `VECTOR` distance functions
- [ ] Filtered vector search combined with secondary indexes
- [ ] Vector-specific benchmarks (recall/latency vs brute force)

## Phase 6: Networking and protocol

- [ ] Server mode (`soldierdb serve`) with a simple wire protocol, or the
      PostgreSQL wire protocol for existing tooling
- [ ] Authentication, TLS and connection/session management
- [ ] Prepared statements with bound parameters (also in the C ABI)

## Phase 7: Tooling and release

- [ ] Finish the YousDB → SoldierDB rename: module path, binary, C ABI,
      clients, docs; keep accepting the `"YOUS"` file magic (#8)
- [ ] CI: `make check` on Linux and macOS, race detector, fuzz smoke run
- [ ] Benchmarks (`go test -bench`) for insert, scan and point lookup
- [ ] Versioned file format with an upgrade path; C ABI versioning
- [ ] Tagged releases with prebuilt shared libraries and CLI binaries
