package kv

import "errors"

var ErrNotFound = errors.New("key not found")

type Pair struct {
	Key   string
	Value []byte
}

type Store interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte)
	Delete(key string) bool
	Scan(start, end string) []Pair
}
