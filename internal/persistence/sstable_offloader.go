package persistence

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sync/singleflight"
)

type SSTableOffloader struct {
	enabled   bool
	minLevel  int
	keepLocal bool

	sstableDir string
	store      ObjectStore
	sf         singleflight.Group
}

func NewSSTableOffloader(options *Options, sstableDir string) (*SSTableOffloader, error) {
	if options == nil || !options.EnableOffloading {
		return &SSTableOffloader{enabled: false}, nil
	}

	minLevel := options.OffloadMinLevel
	if minLevel < 0 {
		minLevel = 0
	}

	var store ObjectStore
	switch options.OffloadBackend {
	case "fs":
		store = NewFileObjectStore(options.OffloadFSRoot)
	case "minio":
		s, err := NewMinioObjectStoreFromOptions(options)
		if err != nil {
			return nil, err
		}
		store = s
	default:
		return nil, fmt.Errorf("unknown offload backend: %s", options.OffloadBackend)
	}

	return &SSTableOffloader{
		enabled:    true,
		minLevel:   minLevel,
		keepLocal:  options.OffloadKeepLocal,
		sstableDir: sstableDir,
		store:      store,
	}, nil
}

func (o *SSTableOffloader) keyFor(fileNum uint64) string {
	return fmt.Sprintf("sstable/%06d.sstable", fileNum)
}

func (o *SSTableOffloader) checksumKeyFor(fileNum uint64) string {
	return o.keyFor(fileNum) + ".crc32"
}

func (o *SSTableOffloader) DeleteRemote(fileNum uint64) error {
	if !o.enabled || o.store == nil {
		return nil
	}
	key := strconv.FormatUint(fileNum, 10)
	_, err, _ := o.sf.Do("del:"+key, func() (interface{}, error) {
		if err := o.store.DeleteObject(o.keyFor(fileNum)); err != nil {
			return nil, err
		}
		if err := o.store.DeleteObject(o.checksumKeyFor(fileNum)); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func (o *SSTableOffloader) localPath(fileNum uint64) string {
	return filepath.Join(o.sstableDir, fmt.Sprintf("%06d.sstable", fileNum))
}

func (o *SSTableOffloader) OffloadIfNeeded(fm *FileMetadata) error {
	if !o.enabled || fm == nil {
		return nil
	}
	if fm.Level < o.minLevel {
		return nil
	}

	key := strconv.FormatUint(fm.FileNum, 10)
	_, err, _ := o.sf.Do("put:"+key, func() (interface{}, error) {
		return nil, o.offloadOne(fm.FileNum)
	})
	return err
}

func (o *SSTableOffloader) offloadOne(fileNum uint64) error {
	local := o.localPath(fileNum)
	f, err := os.Open(local)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	h := crc32.NewIEEE()
	if err := o.store.PutObject(o.keyFor(fileNum), io.TeeReader(f, h)); err != nil {
		return err
	}
	sum := h.Sum32()
	if err := o.store.PutObject(o.checksumKeyFor(fileNum), bytes.NewReader([]byte(fmt.Sprintf("%08x", sum)))); err != nil {
		return err
	}

	if !o.keepLocal {
		_ = os.Remove(local)
	}
	return nil
}

func (o *SSTableOffloader) EnsureLocal(fileNum uint64) error {
	if !o.enabled {
		return os.ErrNotExist
	}

	local := o.localPath(fileNum)
	if _, err := os.Stat(local); err == nil {
		return nil
	}

	key := strconv.FormatUint(fileNum, 10)
	_, err, _ := o.sf.Do("get:"+key, func() (interface{}, error) {
		return nil, o.ensureLocalOne(fileNum)
	})
	if err != nil {
		return err
	}
	if _, err := os.Stat(local); err == nil {
		return nil
	}
	return os.ErrNotExist
}

func (o *SSTableOffloader) ensureLocalOne(fileNum uint64) error {
	local := o.localPath(fileNum)
	if _, err := os.Stat(local); err == nil {
		return nil
	}

	ok, err := o.store.StatObject(o.keyFor(fileNum))
	if err != nil {
		return err
	}
	if !ok {
		return os.ErrNotExist
	}

	expected, hasExpected, err := o.getRemoteChecksum(fileNum)
	if err != nil {
		return err
	}

	rc, err := o.store.GetObject(o.keyFor(fileNum))
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(local), 0750); err != nil {
		return err
	}

	f, err := os.CreateTemp(filepath.Dir(local), filepath.Base(local)+".download-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	h := crc32.NewIEEE()
	_, copyErr := io.Copy(io.MultiWriter(f, h), rc)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}

	if hasExpected && h.Sum32() != expected {
		_ = os.Remove(tmp)
		return fmt.Errorf("sstable checksum mismatch")
	}

	return os.Rename(tmp, local)
}

func (o *SSTableOffloader) getRemoteChecksum(fileNum uint64) (uint32, bool, error) {
	ok, err := o.store.StatObject(o.checksumKeyFor(fileNum))
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, nil
	}
	rc, err := o.store.GetObject(o.checksumKeyFor(fileNum))
	if err != nil {
		return 0, false, err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return 0, false, err
	}
	s := strings.TrimSpace(string(b))
	u, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false, err
	}
	return uint32(u), true, nil
}
