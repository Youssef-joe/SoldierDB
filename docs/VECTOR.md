# Vector search

YousDB supports fixed-dimension `VECTOR` columns and brute-force nearest-neighbor
queries. Components are stored as `float32` values.

```sql
CREATE TABLE documents (
  id INTEGER PRIMARY KEY,
  content TEXT NOT NULL,
  embedding VECTOR(3) NOT NULL
);

INSERT INTO documents VALUES
  (1, 'database recovery', VECTOR(1, 0, 0)),
  (2, 'query planning', VECTOR(0.9, 0.1, 0)),
  (3, 'frontend design', VECTOR(0, 1, 0));

SELECT id, content,
       cosine_distance(embedding, VECTOR(1, 0, 0)) AS distance
FROM documents
ORDER BY distance
LIMIT 2;
```

Supported functions:

- `VECTOR(a, b, ...)` constructs a vector literal.
- `cosine_distance(left, right)` returns `1 - cosine similarity`.
- `euclidean_distance(left, right)` returns the L2 distance.
- `inner_product(left, right)` returns the dot product.

Vectors must have the declared dimension. `NULL` vectors are supported, while
cosine distance rejects zero-length vectors. Search currently scans the table,
so this milestone is intended for small and medium datasets. An HNSW index can
be added later without changing the SQL value format.

The vector is encoded inside the row. It therefore shares the storage engine's
maximum row/value size; very large embedding dimensions will require overflow
pages before they can be stored.
