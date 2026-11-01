package main

import (
	"fmt"

	"github.com/Youssef-joe/YousDB/engine/kv"
)

func main() {
	store := kv.NewMemoryStore()
	store.Set("hello", []byte("database"))
	store.Set("chapter", []byte("kv-store"))

	for _, pair := range store.Scan("", "") {
		fmt.Printf("%s=%s\n", pair.Key, pair.Value)
	}
}
