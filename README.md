# YouseDB

A small Go workspace for building a database from scratch while following
[Build Your Own Database From Scratch in Go](https://build-your-own.org/database/).

The guide starts with a simple KV store, then moves through B+tree storage,
durability, free-list page reuse, transactions, and a SQL-like layer. This repo
is scaffolded to follow that path without locking early experiments into the
final storage engine.

## Requirements

- Go 1.24 or newer
- `make` is optional, but convenient

## Quick Start

```sh
go test ./...
go run ./cmd/myowndb
```

The CLI currently runs a tiny ordered in-memory KV demo. The goal is to replace
that backing store with the B+tree and durable page store as you progress.

## Project Layout

```text
cmd/myowndb/       CLI entrypoint and future manual playground
internal/kv/       KV interface plus simple ordered in-memory implementation
internal/btree/    B+tree notes and initial page/node constants
docs/roadmap.md    Chapter-by-chapter build plan
```

## Suggested Workflow

1. Keep `internal/kv.Store` stable as the small API you test against.
2. Build B+tree nodes in `internal/btree`.
3. Add a file/page layer for durability.
4. Swap the CLI/demo from `kv.MemoryStore` to the durable tree-backed store.
5. Add focused tests before each storage change.

Run this after each milestone:

```sh
make fmt
make test
```
