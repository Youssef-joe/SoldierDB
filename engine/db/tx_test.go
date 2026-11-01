package db

import (
	"errors"
	"fmt"
	"testing"
)

func mustSet(t *testing.T, d *DB, key, value string) {
	t.Helper()
	if err := d.Set([]byte(key), []byte(value)); err != nil {
		t.Fatalf("Set(%q): %v", key, err)
	}
}

func scanKeys(t *testing.T, d *DB, start, end string) []string {
	t.Helper()
	var s, e []byte
	if start != "\x00" {
		s = []byte(start)
	}
	if end != "\x00" {
		e = []byte(end)
	}
	var keys []string
	if err := d.Scan(s, e, func(key, _ []byte) bool {
		keys = append(keys, string(key))
		return true
	}); err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestUpdateCommitsAtomically(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()

	err := d.Update(func(tx *Tx) error {
		if err := tx.Set([]byte("a"), []byte("1")); err != nil {
			return err
		}
		if err := tx.Set([]byte("b"), []byte("2")); err != nil {
			return err
		}
		if err := tx.Set([]byte("c"), []byte("3")); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := scanKeys(t, d, "\x00", "\x00"); fmt.Sprint(got) != "[a b c]" {
		t.Fatalf("keys = %v", got)
	}
	for k, want := range map[string]string{"a": "1", "b": "2", "c": "3"} {
		got, err := d.Get([]byte(k))
		if err != nil || string(got) != want {
			t.Fatalf("Get(%q) = %q, %v", k, got, err)
		}
	}
}

func TestUpdateRollsBackOnError(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	mustSet(t, d, "keep", "original")

	sentinel := errors.New("boom")
	err := d.Update(func(tx *Tx) error {
		if err := tx.Set([]byte("new"), []byte("value")); err != nil {
			return err
		}
		if err := tx.Set([]byte("keep"), []byte("changed")); err != nil {
			return err
		}
		if tx.Delete([]byte("keep")) != true {
			t.Fatal("Delete(keep) = false")
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update error = %v", err)
	}
	if _, err := d.Get([]byte("new")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(new) = %v, want ErrNotFound", err)
	}
	got, err := d.Get([]byte("keep"))
	if err != nil || string(got) != "original" {
		t.Fatalf("Get(keep) = %q, %v", got, err)
	}
}

func TestViewIsReadOnly(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	mustSet(t, d, "a", "1")

	err := d.View(func(tx *Tx) error {
		got, err := tx.Get([]byte("a"))
		if err != nil || string(got) != "1" {
			t.Fatalf("Get(a) = %q, %v", got, err)
		}
		if err := tx.Set([]byte("b"), []byte("2")); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("Set in View = %v, want ErrReadOnly", err)
		}
		if tx.Delete([]byte("a")) {
			t.Fatal("Delete in View = true")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Get([]byte("b")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(b) after View = %v", err)
	}
}

func TestTxSeesOwnWritesAndMergedScan(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	for _, k := range []string{"a", "c", "e"} {
		mustSet(t, d, k, k+"-old")
	}

	err := d.Update(func(tx *Tx) error {
		if err := tx.Set([]byte("b"), []byte("b-new")); err != nil {
			return err
		}
		if err := tx.Set([]byte("c"), []byte("c-new")); err != nil {
			return err
		}
		if !tx.Delete([]byte("e")) {
			t.Fatal("Delete(e) = false")
		}

		got, err := tx.Get([]byte("b"))
		if err != nil || string(got) != "b-new" {
			t.Fatalf("Get(b) = %q, %v", got, err)
		}
		got, err = tx.Get([]byte("c"))
		if err != nil || string(got) != "c-new" {
			t.Fatalf("Get(c) = %q, %v", got, err)
		}
		if _, err := tx.Get([]byte("e")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(e) = %v, want ErrNotFound", err)
		}

		var keys []string
		if err := tx.Scan(nil, nil, func(key, _ []byte) bool {
			keys = append(keys, string(key))
			return true
		}); err != nil {
			return err
		}
		if fmt.Sprint(keys) != "[a b c]" {
			t.Fatalf("tx scan keys = %v", keys)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := scanKeys(t, d, "\x00", "\x00"); fmt.Sprint(got) != "[a b c]" {
		t.Fatalf("committed keys = %v", got)
	}
	got, _ := d.Get([]byte("c"))
	if string(got) != "c-new" {
		t.Fatalf("c = %q", got)
	}
}

func TestTxScanRangeWithBuffer(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	for _, k := range []string{"a", "b", "d", "f"} {
		mustSet(t, d, k, k)
	}

	err := d.Update(func(tx *Tx) error {
		if err := tx.Set([]byte("c"), []byte("c")); err != nil {
			return err
		}
		if err := tx.Set([]byte("e"), []byte("e")); err != nil {
			return err
		}
		if err := tx.Set([]byte("z"), []byte("z")); err != nil {
			return err
		}
		var keys []string
		if err := tx.Scan([]byte("b"), []byte("f"), func(key, _ []byte) bool {
			keys = append(keys, string(key))
			return true
		}); err != nil {
			return err
		}
		if fmt.Sprint(keys) != "[b c d e]" {
			t.Fatalf("range scan = %v", keys)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetThenDeleteInSameTx(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()

	err := d.Update(func(tx *Tx) error {
		if err := tx.Set([]byte("k"), []byte("v")); err != nil {
			return err
		}
		if !tx.Delete([]byte("k")) {
			t.Fatal("Delete after Set = false")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Get([]byte("k")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(k) = %v", err)
	}
}

func TestDeleteThenSetInSameTx(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	mustSet(t, d, "k", "old")

	err := d.Update(func(tx *Tx) error {
		if !tx.Delete([]byte("k")) {
			t.Fatal("Delete = false")
		}
		return tx.Set([]byte("k"), []byte("new"))
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Get([]byte("k"))
	if err != nil || string(got) != "new" {
		t.Fatalf("Get(k) = %q, %v", got, err)
	}
}

func TestTxDoneAfterCallback(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()

	var tx *Tx
	if err := d.Update(func(t *Tx) error {
		tx = t
		return t.Set([]byte("k"), []byte("v"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Set([]byte("k2"), []byte("v2")); !errors.Is(err, ErrTxDone) {
		t.Fatalf("Set after Update = %v, want ErrTxDone", err)
	}
	if _, err := tx.Get([]byte("k")); !errors.Is(err, ErrTxDone) {
		t.Fatalf("Get after Update = %v, want ErrTxDone", err)
	}
}

func TestEmptyUpdateDoesNotCommit(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	before := d.Stats().TxID
	if err := d.Update(func(tx *Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if after := d.Stats().TxID; after != before {
		t.Fatalf("txid changed from %d to %d for an empty update", before, after)
	}
}

func TestLargeTransaction(t *testing.T) {
	d, path := openTemp(t)
	const n = 1500
	err := d.Update(func(tx *Tx) error {
		for i := 0; i < n; i++ {
			key := []byte(fmt.Sprintf("key-%05d", i))
			value := []byte(fmt.Sprintf("value-%05d", i))
			if err := tx.Set(key, value); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	stats := d.Stats()
	if stats.TxID != 1 {
		t.Fatalf("txid = %d, want 1 (one transaction)", stats.TxID)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	d2, err := OpenWithOptions(path, Options{NoSync: true})
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%05d", i))
		got, err := d2.Get(key)
		if err != nil || string(got) != fmt.Sprintf("value-%05d", i) {
			t.Fatalf("Get(%q) = %q, %v", key, got, err)
		}
	}
}

func TestTxDeleteMissing(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	err := d.Update(func(tx *Tx) error {
		if tx.Delete([]byte("missing")) {
			t.Fatal("Delete(missing) = true")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Get([]byte("missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) = %v", err)
	}
}
