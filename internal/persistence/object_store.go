package persistence

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ObjectStore interface {
	PutObject(key string, r io.Reader) error
	GetObject(key string) (io.ReadCloser, error)
	StatObject(key string) (bool, error)
	DeleteObject(key string) error
}

type FileObjectStore struct {
	root string
}

func NewFileObjectStore(root string) *FileObjectStore {
	return &FileObjectStore{root: root}
}

func fileObjectPath(root, key string) (string, error) {
	k := filepath.Clean(filepath.FromSlash(key))
	k = strings.TrimPrefix(k, string(filepath.Separator))
	if k == "" || k == "." || k == ".." {
		return "", fmt.Errorf("invalid object key")
	}
	if filepath.IsAbs(k) {
		return "", fmt.Errorf("invalid object key")
	}
	if strings.HasPrefix(k, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid object key")
	}
	rootClean := filepath.Clean(root)
	full := filepath.Join(rootClean, k)
	rel, err := filepath.Rel(rootClean, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid object key")
	}
	return full, nil
}

func (s *FileObjectStore) PutObject(key string, r io.Reader) error {
	path, err := fileObjectPath(s.root, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (s *FileObjectStore) GetObject(key string) (io.ReadCloser, error) {
	path, err := fileObjectPath(s.root, key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *FileObjectStore) StatObject(key string) (bool, error) {
	path, err := fileObjectPath(s.root, key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *FileObjectStore) DeleteObject(key string) error {
	path, err := fileObjectPath(s.root, key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}
