# YousDB

A relational database built from scratch: a crash-safe page store, a
copy-on-write B+tree, transactions, and a SQL layer — written in Go and exposed
to Rust and C++ through a stable C ABI.

YousDB follows [Build Your Own Database From Scratch in Go](https://build-your-own.org/database/)
for the storage engine, then extends it with a real SQL front end.

## Status

The storage engine and SQL layer are under active construction. See
[`docs/ENGINE_PLAN.md`](docs/ENGINE_PLAN.md) for the milestone breakdown and
[`docs/roadmap.md`](docs/roadmap.md) for the chapter-by-chapter study notes.

## Requirements

- Go 1.24 or newer
- Rust 1.80+ and Cargo (for the Rust client)
- A C++20 compiler and CMake 3.20+ (for the C++ client)
- `make` is optional, but convenient

## Quick Start

```sh
make test          # run the Go test suite
make run           # build and start the SQL shell
```

The shell accepts SQL terminated by `;`:

```sql
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, age INTEGER);
INSERT INTO users VALUES (1, 'Ada', 36), (2, 'Grace', 45);
SELECT name, age FROM users WHERE age > 40 ORDER BY name;
```

## Project Layout

```text
cmd/yousdb/        SQL shell / CLI
engine/pager/      fixed-size page file, meta pages, crash recovery
engine/btree/      copy-on-write B+tree
engine/db/         durable KV facade and transactions
engine/kv/         Store interface and in-memory oracle
engine/sql/        lexer, parser, catalog, executor
capi/              C ABI shared library
clients/rust/      Rust client crate and CLI
clients/cpp/       C++ client and CMake project
docs/              design notes and roadmap
```

## Build Targets

```sh
make fmt           # gofmt the Go sources
make test          # Go tests
make build         # build the SQL CLI
make shared        # build libyousdb shared library and header
make rust          # build the Rust client
make cpp           # build the C++ client
make check         # fmt check + Go tests + client tests
```

## Design Notes

- Pages are 4096 bytes; pages 0 and 1 are alternating checksummed meta pages.
- Commits write the inactive meta page last and `fsync` it, giving atomic
  visibility of a new tree root.
- The tree never overwrites a page reachable from a committed root
  (copy-on-write), and freed pages are recycled through a persistent free list.
- Transactions buffer writes and publish them in a single atomic commit.
