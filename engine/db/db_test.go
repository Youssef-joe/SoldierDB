package db

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/Youssef-joe/YousDB/engine/kv"
)

func openTemp(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenWithOptions(path, Options{NoSync: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return d, path
}

func TestSetGetDelete(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()

	if _, err := d.Get([]byte("missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing error = %v, want ErrNotFound", err)
	}
	if err := d.Set([]byte("alpha"), []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := d.Set([]byte("beta"), []byte("two")); err != nil {
		t.Fatal(err)
	}
	got, err := d.Get([]byte("alpha"))
	if err != nil || string(got) != "one" {
		t.Fatalf("Get(alpha) = %q, %v", got, err)
	}

	// Mutating the returned slice must not affect the store.
	got[0] = 'X'
	got, _ = d.Get([]byte("alpha"))
	if string(got) != "one" {
		t.Fatalf("stored value mutated through Get: %q", got)
	}

	removed, err := d.Delete([]byte("alpha"))
	if err != nil || !removed {
		t.Fatalf("Delete(alpha) = %v, %v", removed, err)
	}
	removed, err = d.Delete([]byte("alpha"))
	if err != nil || removed {
		t.Fatalf("second Delete(alpha) = %v, %v", removed, err)
	}
	if _, err := d.Get([]byte("alpha")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete = %v", err)
	}
}

func TestReopenPersistence(t *testing.T) {
	d, path := openTemp(t)
	for i := 0; i < 1000; i++ {
		if err := d.Set([]byte(fmt.Sprintf("key-%04d", i)), []byte(fmt.Sprintf("value-%04d", i))); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 1000; i += 4 {
		if _, err := d.Delete([]byte(fmt.Sprintf("key-%04d", i))); err != nil {
			t.Fatal(err)
		}
	}
	before, err := d.Pairs(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stats := d.Stats()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	d2, err := OpenWithOptions(path, Options{NoSync: true})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer d2.Close()

	after, err := d2.Pairs(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("pair count = %d, want %d", len(after), len(before))
	}
	for i := range before {
		if before[i].Key != after[i].Key || !bytes.Equal(before[i].Value, after[i].Value) {
			t.Fatalf("pair %d = %q=%q, want %q=%q", i, after[i].Key, after[i].Value, before[i].Key, before[i].Value)
		}
	}
	if d2.Stats().TxID != stats.TxID {
		t.Fatalf("txid after reopen = %d, want %d", d2.Stats().TxID, stats.TxID)
	}
}

func TestScanRange(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		if err := d.Set([]byte(k), []byte(k+"!")); err != nil {
			t.Fatal(err)
		}
	}

	pairs, err := d.Pairs([]byte("b"), []byte("d"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[0].Key != "b" || pairs[1].Key != "c" {
		t.Fatalf("range pairs = %+v", pairs)
	}

	var keys []string
	if err := d.Scan(nil, nil, func(key, _ []byte) bool {
		keys = append(keys, string(key))
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(keys) != "[a b c d e]" {
		t.Fatalf("scan keys = %v", keys)
	}

	count := 0
	if err := d.Scan(nil, nil, func(_, _ []byte) bool {
		count++
		return count < 2
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("early stop count = %d", count)
	}
}

func TestRandomOracleAndReopen(t *testing.T) {
	d, path := openTemp(t)
	oracle := kv.NewMemoryStore()
	rng := rand.New(rand.NewSource(1234))

	for i := 0; i < 2500; i++ {
		key := fmt.Sprintf("key-%03d", rng.Intn(300))
		if rng.Intn(2) == 0 {
			val := fmt.Sprintf("value-%d", i)
			if err := d.Set([]byte(key), []byte(val)); err != nil {
				t.Fatal(err)
			}
			oracle.Set(key, []byte(val))
		} else {
			want := oracle.Delete(key)
			got, err := d.Delete([]byte(key))
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("step %d: Delete(%q) = %v, want %v", i, key, got, want)
			}
		}

		if i%50 == 0 {
			compareWithOracle(t, d, oracle)
		}
	}
	compareWithOracle(t, d, oracle)

	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d2, err := OpenWithOptions(path, Options{NoSync: true})
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	compareWithOracle(t, d2, oracle)
}

func compareWithOracle(t *testing.T, d *DB, oracle *kv.MemoryStore) {
	t.Helper()
	pairs, err := d.Pairs(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := oracle.Scan("", "")
	if len(pairs) != len(want) {
		t.Fatalf("pair count = %d, want %d", len(pairs), len(want))
	}
	for i := range want {
		if pairs[i].Key != want[i].Key || !bytes.Equal(pairs[i].Value, want[i].Value) {
			t.Fatalf("pair %d = %q=%q, want %q=%q", i, pairs[i].Key, pairs[i].Value, want[i].Key, want[i].Value)
		}
	}
}

func TestFreeListReuse(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()

	for i := 0; i < 400; i++ {
		value := make([]byte, 200)
		for j := range value {
			value[j] = byte(i)
		}
		if err := d.Set([]byte("hot-key"), value); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Delete([]byte("hot-key")); err != nil {
			t.Fatal(err)
		}
	}

	stats := d.Stats()
	if stats.Pages > 16 {
		t.Fatalf("page count grew to %d after repeated set/delete; free list is not reusing pages", stats.Pages)
	}
	if stats.FreePages == 0 {
		t.Fatal("free list is empty; pages were not reclaimed")
	}
	if err := d.Set([]byte("hot-key"), []byte("final")); err != nil {
		t.Fatal(err)
	}
	got, err := d.Get([]byte("hot-key"))
	if err != nil || string(got) != "final" {
		t.Fatalf("Get after churn = %q, %v", got, err)
	}
}

func TestOversizedValueDoesNotCorrupt(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()

	if err := d.Set([]byte("ok"), []byte("value")); err != nil {
		t.Fatal(err)
	}
	if err := d.Set([]byte("too-big"), make([]byte, 4000)); err == nil {
		t.Fatal("Set with oversized value succeeded")
	}
	if err := d.Set([]byte("still-ok"), []byte("value2")); err != nil {
		t.Fatalf("Set after failed write: %v", err)
	}
	got, err := d.Get([]byte("ok"))
	if err != nil || string(got) != "value" {
		t.Fatalf("Get(ok) = %q, %v", got, err)
	}
}

func TestClosedDatabase(t *testing.T) {
	d, _ := openTemp(t)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := d.Set([]byte("k"), []byte("v")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Set after close = %v", err)
	}
	if _, err := d.Get([]byte("k")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Get after close = %v", err)
	}
}
