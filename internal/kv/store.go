package kv

import (
	"errors"
	"fmt"
	"os"
	"math/rand/v2"
)

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

func SaveData1(path string, data []byte) error {
	fp, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0664);
	if err != nil {
		return err;
	}
	defer fp.Close();

	_,err = fp.Write(data);
	if err != nil {
		return err;
	}

	return fp.Sync(); //fsync
}

func SaveData2(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.tmp.%d", path, rand.Int());
	fp, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0664);

	if err != nil {
		return err;
	}

	defer func() {
		fp.Close();
		if err != nil {
			os.Remove(tmp);
		}
	}()

	_,err = fp.Write(data);

	if err != nil {
		return err;
	}

	return os.Rename(tmp, path);
}
