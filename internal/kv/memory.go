package kv

import (
	"sort"
)

type MemoryStore struct {
	data map[string][]byte
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string][]byte)}
}

func (s *MemoryStore) Get(key string) ([]byte, error) {
	value, ok := s.data[key]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(value), nil
}

func (s *MemoryStore) Set(key string, value []byte) {
	s.data[key] = clone(value)
}

func (s *MemoryStore) Delete(key string) bool {
	if _, ok := s.data[key]; !ok {
		return false
	}
	delete(s.data, key)
	return true
}

func (s *MemoryStore) Scan(start, end string) []Pair {
	keys := make([]string, 0, len(s.data))
	for key := range s.data {
		if key >= start && (end == "" || key < end) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	pairs := make([]Pair, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, Pair{Key: key, Value: clone(s.data[key])})
	}
	return pairs
}

func clone(value []byte) []byte {
	if value == nil {
		return nil
	}
	out := make([]byte, len(value))
	copy(out, value)
	return out
}
