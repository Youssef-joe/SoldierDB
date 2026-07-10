# Build Roadmap

Reference: <https://build-your-own.org/database/>

The public web version covers Part I, a simple KV store. Part II is listed by
the author as ebook/paperback content, so this roadmap keeps the public chapters
first and leaves later relational work as extension points.

## 0. Introduction

- Keep the project small and test-driven.
- Treat the KV API as the contract that later storage engines must satisfy.

## 1. From Files To Databases

- Add a file format package for fixed-size pages.
- Write and read pages by page number.
- Introduce crash-focused tests using temporary files.

## 2. Indexing Data Structures

- Compare the current ordered memory store with the B+tree target.
- Add search helpers for sorted keys inside a node.

## 3. B-Tree & Crash Recovery

- Define node layout and page constants.
- Make tree updates copy-on-write friendly.
- Start tracking root page changes.

## 4. B+Tree Node and Insertion

- Implement leaf/internal node encoding.
- Implement lookup and insertion.
- Add split tests with many keys and variable value sizes.

## 5. B+Tree Deletion and Testing

- Implement delete.
- Handle rebalancing or page reclamation strategy.
- Add randomized tests against `kv.MemoryStore` as the oracle.

## 6. Append-Only KV Store

- Persist tree pages append-only.
- Add open/close behavior and recovery from the latest root.

## 7. Free List: Recycle & Reuse

- Track reusable pages.
- Reuse freed pages safely after commits.
- Add tests that insert, delete, close, reopen, and validate data.

## Later Extensions

- Tables on top of KV encoding.
- Range queries and secondary indexes.
- Atomic transactions and concurrency control.
- SQL-like parser and query execution.
