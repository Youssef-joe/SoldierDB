# Contributing to SoldierDB

Thanks for your interest. SoldierDB is an **educational database**: we build
every layer by hand (pages, a B+tree, transactions, SQL, vector search) so
we can understand how real databases work. You don't need database
experience to contribute. If you can write Go and like reading papers, you
will fit right in, and you'll come out knowing how a database works.

## Why contribute

- **Learn database internals by shipping code.** Each roadmap issue lists
  what you'll learn and what to read before you start.
- **Small, readable codebase.** The whole engine is a few thousand lines of
  Go under `engine/`, with no frameworks to learn.
- **Real reviews.** Every PR gets a careful review focused on correctness,
  which is the most useful feedback for learning systems programming.
- **Every layer is open.** Storage, query planning, concurrency, networking,
  vector indexes, and Rust/C++ clients.

## Find something to work on

1. **[Good first issues](https://github.com/Youssef-joe/SoldierDB/labels/good%20first%20issue)**:
   small, well-scoped tasks.
2. **[Phase 1 issues](https://github.com/Youssef-joe/SoldierDB/labels/phase-1)**:
   correctness bugs and tests. This is the most valuable work right now.
3. **[ROADMAP.md](ROADMAP.md)**: the full plan, phase by phase, with every
   item linked to an issue.

Before you start, **comment on the issue** so we can assign it to you and no
two people do the same work. If an issue has gone quiet for two weeks after
someone claimed it, ask in a comment and you can take it over.

For anything large (a new storage feature, MVCC, a planner, a wire
protocol), open a short design discussion in the issue first. Issues labeled
`research` need a design note in `docs/` before any code.

## Set up

Requirements: Go 1.24+. For the clients you also need Rust 1.70+ and a C++20
compiler with CMake 3.20+.

```sh
git clone https://github.com/<you>/SoldierDB.git
cd SoldierDB
make test     # Go tests
make run      # start the SQL shell
make check    # everything: gofmt, Go tests, C ABI smoke test, Rust and C++ clients
```

## Map of the code

Read it bottom-up. Each layer only depends on the ones below it.

| Layer | Path | Read first |
|---|---|---|
| Pager: 4 KiB pages, dual meta pages, free list | `engine/pager/` | `pager.go` header comment |
| Copy-on-write B+tree | `engine/btree/` | `node.go`, then `tree.go` |
| Transactions and KV API | `engine/db/` | `db.go`, `tx.go` |
| In-memory oracle used by tests | `engine/kv/` | `memory.go` |
| SQL: lexer, parser, catalog, executor | `engine/sql/` | `parser.go`, `exec.go` |
| C ABI | `capi/` | `capi/test/smoke.c` |
| Clients | `clients/rust/`, `clients/cpp/` | `clients/rust/src/`, `clients/cpp/CMakeLists.txt` |

Background: SoldierDB follows
[Build Your Own Database From Scratch in Go](https://build-your-own.org/database/)
for the storage engine. [`docs/ENGINE_PLAN.md`](docs/ENGINE_PLAN.md) has the
milestone design, [`docs/VECTOR.md`](docs/VECTOR.md) covers vector search.

## Making a change

1. Fork, then branch from `main` (`fix/torn-trailing-page`, `feat/create-index`).
2. Keep each PR to **one logical change**. Smaller PRs get reviewed faster.
3. **Tests are required.**
   - Bug fix: add a test that fails before your fix and passes after.
   - Storage changes: check results against `kv.MemoryStore` and keep the
     tree invariant checks (`engine/btree/invariants_test.go`) passing.
   - Concurrency changes: run `go test -race ./...`.
4. Run `make fmt` and `make check` before pushing.
5. Open a PR using the template, and link the issue (`Closes #123`).

### Rules that protect your data (and your PR)

- **Never panic on valid input or on bytes read from disk.** Return an
  error. A panic in the engine crashes every program that embeds it.
- **On-disk format changes must be called out** in the PR. Old files must
  either still open or fail with a clear error (see #34).
- **C ABI changes must be called out** too, since Rust and C++ clients
  depend on it.
- **Keep the write order of commits intact:** data pages, then fsync, then
  the meta page. If you touch the pager, explain why your change is
  crash-safe.

### Style

- Standard `gofmt`; no extra linters required.
- Prefer clear code over clever code. This repo is read by people learning.
- Comment *why*, not *what*. A link to the paper or doc section you followed
  is a great comment.
- Commit messages: short imperative summary line (`pager: tolerate torn
  trailing page`), then a body explaining why if it isn't obvious.

## Reporting bugs

Use the **Bug report** template. The best reports include a minimal SQL
script or Go test that reproduces the problem. Data loss and crash bugs are
the highest priority.

## Asking questions

Open an issue with your question. No question about databases or the code is
too basic. If something in the code confused you, that's a sign the code or
docs should improve, and saying so is itself a contribution.

## Code of conduct

Be kind and patient. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## License

SoldierDB is MIT-licensed. By contributing, you agree your contributions are
licensed under the [MIT License](LICENSE).
