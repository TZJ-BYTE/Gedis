package persistence

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestTableCache_EvictDoesNotBreakActiveIterator(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultOptions()
	opts.UseCache = false
	opts.EnableOffloading = false

	path1 := filepath.Join(dir, "000001.sstable")
	b1, err := NewSSTableBuilder(path1, opts)
	if err != nil {
		t.Fatalf("builder1: %v", err)
	}
	if err := b1.Add([]byte("a"), []byte("1")); err != nil {
		t.Fatalf("add1: %v", err)
	}
	if err := b1.Add([]byte("b"), []byte("2")); err != nil {
		t.Fatalf("add1: %v", err)
	}
	if err := b1.Finish(); err != nil {
		t.Fatalf("finish1: %v", err)
	}

	path2 := filepath.Join(dir, "000002.sstable")
	b2, err := NewSSTableBuilder(path2, opts)
	if err != nil {
		t.Fatalf("builder2: %v", err)
	}
	if err := b2.Add([]byte("c"), []byte("3")); err != nil {
		t.Fatalf("add2: %v", err)
	}
	if err := b2.Add([]byte("d"), []byte("4")); err != nil {
		t.Fatalf("add2: %v", err)
	}
	if err := b2.Finish(); err != nil {
		t.Fatalf("finish2: %v", err)
	}

	c := NewTableCache(1)
	defer c.Close()

	r1, err := c.GetOrOpen(1, func() (*SSTableReader, error) {
		return OpenSSTableForReadWithCache(1, path1, opts, nil)
	})
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	it := r1.NewIterator()
	defer it.Release()

	r2, err := c.GetOrOpen(2, func() (*SSTableReader, error) {
		return OpenSSTableForReadWithCache(2, path2, opts, nil)
	})
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	_ = r2

	var got [][]byte
	for it.First(); it.Valid(); it.Next() {
		got = append(got, append([]byte(nil), it.Key()...))
	}
	if it.Error() != nil {
		t.Fatalf("iter err: %v", it.Error())
	}
	if len(got) != 2 || !bytes.Equal(got[0], []byte("a")) || !bytes.Equal(got[1], []byte("b")) {
		t.Fatalf("got=%q", got)
	}
}
