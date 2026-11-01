# YousDB Engine Plan

YousDB is a from-scratch relational database. The storage engine and SQL layer
are written in Go; Rust and C++ consume the engine through a stable C ABI.

The work is split into independent, reviewable milestones. Each milestone is a
single commit and leaves `make test` green.

## Architecture

```text
+-------------------------------------------------------------+
|  cmd/yousdb          SQL shell / REPL (Go)                  |
+-------------------------------------------------------------+
|  engine/sql          lexer, parser, catalog, planner, exec  |
+-------------------------------------------------------------+
|  engine/db           transactions, durable KV facade        |
+-------------------------------------------------------------+
|  engine/btree        B+tree nodes, search, insert, delete   |
+-------------------------------------------------------------+
|  engine/pager        fixed-size pages, meta pages, fsync    |
+-------------------------------------------------------------+
|  engine/kv           Store interface + in-memory oracle     |
+-------------------------------------------------------------+

capi/                  C ABI (c-shared) over engine/sql
clients/rust           safe Rust wrapper + CLI
clients/cpp            RAII C++ wrapper + CLI
```

### Storage invariants

- The file is an array of 4096-byte pages.
- Pages 0 and 1 are alternating meta pages. A commit writes the inactive meta
  page last and fsyncs it, so a crash always leaves one valid meta page.
- Every meta page carries a magic value, a format version, a transaction id and
  a CRC32 checksum. Recovery selects the valid page with the highest txid.
- The B+tree is copy-on-write: a mutation never overwrites a page reachable
  from a committed meta page.
- Freed pages are recorded in a persistent free list and reused before the file
  grows.
- A transaction buffers its writes in memory. Commit applies them as one
  copy-on-write batch and publishes a single meta page.

## Milestones

### 0. Repository foundation

- Restructure into `engine/`, `cmd/`, `capi/`, `clients/`.
- Replace the non-compiling B+tree scaffold.
- Rename the module to `github.com/Youssef-joe/YousDB`.
- Add this plan, build targets and ignore rules.

Commit: `chore: restructure repository for engine and polyglot clients`

### 1. Pager and crash-safe meta pages

- `engine/pager`: page file, in-memory page cache, allocation, fsync.
- Dual meta pages with checksums and txid-based recovery.
- Tests: reopen, torn/invalid meta fallback, page round-trip.

Commit: `feat(pager): add crash-safe page file and meta pages`

### 2. B+tree nodes, lookup, insert and scan

- Node encoding: type, key count, child pointers, offsets, key/value payload.
- Binary search inside a node; leaf and internal lookup.
- Insertion with node splitting and root growth.
- Ordered range scan.
- Oracle tests against the in-memory store.

Commit: `feat(btree): implement node format, lookup, insert and scan`

### 3. Deletion, free list and durable KV store

- Rebalancing delete with underflow handling and root shrink.
- Persistent free list with page reuse.
- `engine/db`: `Open`, `Close`, `Get`, `Set`, `Delete`, `Scan`.
- Crash/reopen and randomized differential tests.

Commit: `feat(db): add durable KV store with deletion and free-list reuse`

### 4. Transactions

- `View` / `Update` transactions with snapshot reads.
- Buffered writes, atomic commit, rollback on error.
- Isolation and conflict tests.

Commit: `feat(db): add transactions with atomic commit`

### 5. SQL front end

- Lexer: keywords, identifiers, literals, operators, comments.
- AST and recursive-descent parser.
- Statements: `CREATE TABLE`, `DROP TABLE`, `INSERT`, `SELECT`, `UPDATE`,
  `DELETE`, `BEGIN`, `COMMIT`, `ROLLBACK`, `SHOW TABLES`, `DESCRIBE`.

Commit: `feat(sql): add lexer, AST and recursive-descent parser`

### 6. Catalog, executor and SQL shell

- System catalog persisted in the KV store.
- Typed row encoding (`INTEGER`, `REAL`, `TEXT`, `BOOLEAN`, `NULL`).
- Executor: filtering, projection, ordering, limits, aggregates, grouping,
  primary-key and UNIQUE constraints, defaults and auto-rowids.
- Interactive shell and `-c` one-shot mode.

Commit: `feat(sql): add catalog, executor and interactive shell`

### 7. C ABI

- `capi/`: `yousdb_open`, `yousdb_close`, `yousdb_exec`, `yousdb_query`,
  result/row accessors, error accessors.
- `-buildmode=c-shared` output and generated header.
- C smoke test.

Commit: `feat(capi): expose the SQL engine through a stable C ABI`

### 8. Rust client

- Safe wrapper crate with RAII handles and typed values.
- CLI and integration tests against the shared library.

Commit: `feat(rust): add safe Rust client and CLI`

### 9. C++ client

- Header-only RAII wrapper, CMake build, CLI.
- Integration test against the shared library.

Commit: `feat(cpp): add C++ client, CMake build and CLI`

## Testing strategy

- Unit tests per package, table-driven where practical.
- Differential tests: random operations against `kv.MemoryStore` as oracle.
- Crash tests: simulate torn writes by corrupting meta pages and reopening.
- SQL tests: parse and execute every supported statement shape.
- Cross-language tests: run the same query suite from Go, C, Rust and C++.

## Deliberate non-goals (for now)

- Multi-process concurrency and network replication.
- Cost-based query optimization.
- MVCC; transactions are serialized with a single writer.
- Secondary indexes and a cost-based planner; queries scan the primary tree.
- Full SQL standard: joins, subqueries and window functions are future work.
