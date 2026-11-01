package kv

import (
	"errors"
	"testing"
)

func TestMemoryStoreGetSetAndDelete(t *testing.T) {
	store := NewMemoryStore()

	store.Set("alpha", []byte("one"))

	got, err := store.Get("alpha")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got) != "one" {
		t.Fatalf("Get = %q, want %q", got, "one")
	}

	got[0] = 'x'
	got, err = store.Get("alpha")
	if err != nil {
		t.Fatalf("Get returned error after mutation: %v", err)
	}
	if string(got) != "one" {
		t.Fatalf("stored value was mutated through returned slice: %q", got)
	}

	if !store.Delete("alpha") {
		t.Fatal("Delete returned false for existing key")
	}
	if _, err := store.Get("alpha"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreScanReturnsSortedRange(t *testing.T) {
	store := NewMemoryStore()
	store.Set("gamma", []byte("3"))
	store.Set("alpha", []byte("1"))
	store.Set("beta", []byte("2"))
	store.Set("delta", []byte("4"))

	got := store.Scan("beta", "gamma")
	want := []Pair{
		{Key: "beta", Value: []byte("2")},
		{Key: "delta", Value: []byte("4")},
	}

	if len(got) != len(want) {
		t.Fatalf("Scan returned %d pairs, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Key != want[i].Key || string(got[i].Value) != string(want[i].Value) {
			t.Fatalf("Scan[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
