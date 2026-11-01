package db

import (
	"errors"
	"testing"
)

func TestExplicitTransactionCommit(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()

	tx, err := d.Begin(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Set([]byte("b"), []byte("2")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	for k, want := range map[string]string{"a": "1", "b": "2"} {
		got, err := d.Get([]byte(k))
		if err != nil || string(got) != want {
			t.Fatalf("Get(%q) = %q, %v", k, got, err)
		}
	}
	if err := tx.Commit(); !errors.Is(err, ErrTxDone) {
		t.Fatalf("second Commit = %v, want ErrTxDone", err)
	}
}

func TestExplicitTransactionRollback(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	mustSet(t, d, "keep", "old")

	tx, err := d.Begin(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Set([]byte("keep"), []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Set([]byte("added"), []byte("value")); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	tx.Rollback() // idempotent

	got, err := d.Get([]byte("keep"))
	if err != nil || string(got) != "old" {
		t.Fatalf("Get(keep) = %q, %v", got, err)
	}
	if _, err := d.Get([]byte("added")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(added) = %v", err)
	}
}

func TestExplicitReadTransaction(t *testing.T) {
	d, _ := openTemp(t)
	defer d.Close()
	mustSet(t, d, "k", "v")

	tx, err := d.Begin(false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tx.Get([]byte("k"))
	if err != nil || string(got) != "v" {
		t.Fatalf("Get(k) = %q, %v", got, err)
	}
	if err := tx.Set([]byte("x"), []byte("y")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Set in read tx = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
