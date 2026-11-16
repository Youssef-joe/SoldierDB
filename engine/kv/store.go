package kv

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"

	"github.com/Youssef-joe/SoldierDB/engine/fsutil"
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
	fp, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0664)
	if err != nil {
		return err
	}
	defer fp.Close()

	_, err = fp.Write(data)
	if err != nil {
		return err
	}

	if err := fp.Sync(); err != nil {
		return err
	}
	// The file data is on disk; sync the directory so the entry that names a
	// newly created file is durable as well.
	return fsutil.SyncDir(path)
}

func SaveData2(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.tmp.%d", path, rand.Int())
	fp, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0664)

	if err != nil {
		return err
	}

	_, err = fp.Write(data)

	if err != nil {
		fp.Close()
		os.Remove(tmp)
		return err
	}

	// Flush and close the temporary file before the rename: the data has to be
	// on disk under the new name, and Windows refuses to rename a file that is
	// still open.
	if err = fp.Sync(); err != nil {
		fp.Close()
		os.Remove(tmp)
		return err
	}
	if err = fp.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	if err = os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	// The rename is atomic, but the new name lives in the directory cache until
	// the directory itself is synced, so durability needs one more fsync.
	return fsutil.SyncDir(path)
}
