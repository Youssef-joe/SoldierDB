<p align="center">
  <img src="site/logo.svg" width="120" alt="SoldierDB badge" />
</p>

<h1 align="center">SoldierDB</h1>

<p align="center"><b>Holds the line on your data.</b><br/>
<a href="https://youssef-joe.github.io/SoldierDB/">Website</a> · <a href="ROADMAP.md">Roadmap</a> · <a href="https://github.com/Youssef-joe/SoldierDB/issues">Issues</a></p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT license" /></a>
  <a href="https://github.com/Youssef-joe/SoldierDB/labels/good%20first%20issue"><img src="https://img.shields.io/github/issues/Youssef-joe/SoldierDB/good%20first%20issue?label=good%20first%20issues&color=7057ff" alt="Good first issues" /></a>
  <a href="CONTRIBUTING.md"><img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg" alt="PRs welcome" /></a>
</p>

A relational database built from scratch: a crash-safe page store, a
copy-on-write B+tree, transactions, and a SQL layer — written in Go and exposed
to Rust and C++ through a stable C ABI.

SoldierDB follows [Build Your Own Database From Scratch in Go](https://build-your-own.org/database/)
for the storage engine, then extends it with a real SQL front end.

## Status

The storage engine and SQL layer are under active construction. See
[`docs/ENGINE_PLAN.md`](docs/ENGINE_PLAN.md) for the milestone breakdown and
[`docs/roadmap.md`](docs/roadmap.md) for the chapter-by-chapter study notes.

## Requirements

- Go 1.24 or newer
- Rust 1.70+ and Cargo (for the Rust client)
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
cmd/soldierdb/     SQL shell / CLI
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
make shared        # build libsoldierdb shared library and header
make rust          # build the Rust client
make cpp           # build the C++ client
make check         # fmt check + Go tests + C, Rust and C++ client tests
```

## Vector search

SoldierDB supports fixed-dimension `VECTOR` columns and brute-force similarity
search. See [`docs/VECTOR.md`](docs/VECTOR.md) for the full syntax. A minimal
query looks like this:

```sql
CREATE TABLE docs (id INTEGER PRIMARY KEY, embedding VECTOR(3));
SELECT id FROM docs
ORDER BY cosine_distance(embedding, VECTOR(1, 0, 0))
LIMIT 10;
```

## Design Notes

- Pages are 4096 bytes; pages 0 and 1 are alternating checksummed meta pages.
- Commits write the inactive meta page last and `fsync` it, giving atomic
  visibility of a new tree root.
- A database file that is initialized on open syncs its parent directory after
  the first meta pages are written, and `kv.SaveData2` does the same after its
  temp-file rename, so the directory entry is durable too and the file cannot
  come back nameless after a power cut.
- The tree never overwrites a page reachable from a committed root
  (copy-on-write), and freed pages are recycled through a persistent free list.
- Transactions buffer writes and publish them in a single atomic commit.

## Contributing

SoldierDB is an educational project, and it's built in the open. Working on
it is one of the most direct ways to learn how a database works, from fsync
ordering to query planning. Every
[roadmap](ROADMAP.md) item is a GitHub issue with a goal, the code involved,
what you'll learn, and what to read.

- New here? Start with a [good first issue](https://github.com/Youssef-joe/SoldierDB/labels/good%20first%20issue).
- Want the most impact? Take a [Phase 1](https://github.com/Youssef-joe/SoldierDB/labels/phase-1) correctness issue.
- Read [CONTRIBUTING.md](CONTRIBUTING.md) for setup, a map of the code, and how PRs are reviewed.

## License

[MIT](LICENSE)
