//go:build windows

package rustengine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

type SyncPolicy uint32

const (
	SyncNone SyncPolicy = iota
	SyncFlush
	SyncData
)

type Options struct {
	DataDir            string
	SegmentSizeBytes   uint64
	CheckpointAfterOps uint64
	SyncPolicy         SyncPolicy
}

type Stats struct {
	Keys            uint64
	ActiveSegmentID uint64
	SegmentCount    uint64
	CachedReaders   uint64
	PinnedSegments  uint64
	PendingReclaims uint64
}

type redigoBuf struct {
	Ptr uintptr
	Len uintptr
}

type redigoStats struct {
	Keys            uint64
	ActiveSegmentID uint64
	SegmentCount    uint64
	CachedReaders   uint64
	PinnedSegments  uint64
	PendingReclaims uint64
}

type dllAPI struct {
	dll             *syscall.DLL
	bufFree         *syscall.Proc
	engineOpen      *syscall.Proc
	engineClose     *syscall.Proc
	enginePut       *syscall.Proc
	engineGet       *syscall.Proc
	engineDelete    *syscall.Proc
	engineCompact   *syscall.Proc
	engineStats     *syscall.Proc
	engineIterOpen  *syscall.Proc
	engineIterNext  *syscall.Proc
	engineIterClose *syscall.Proc
}

type Engine struct {
	handle uintptr
	opts   Options
}

var (
	loadOnce sync.Once
	loadErr  error
	api      *dllAPI
)

func Open(opts Options) (*Engine, error) {
	if err := ensureDLL(); err != nil {
		return nil, err
	}
	if opts.DataDir == "" {
		return nil, errors.New("rust engine data dir is required")
	}
	if opts.SegmentSizeBytes == 0 {
		opts.SegmentSizeBytes = 64 << 20
	}
	if opts.CheckpointAfterOps == 0 {
		opts.CheckpointAfterOps = 1024
	}

	pathBytes := append([]byte(opts.DataDir), 0)
	var errBuf redigoBuf
	handle, _, _ := api.engineOpen.Call(
		uintptr(unsafe.Pointer(&pathBytes[0])),
		uintptr(opts.SegmentSizeBytes),
		uintptr(opts.CheckpointAfterOps),
		uintptr(opts.SyncPolicy),
		uintptr(unsafe.Pointer(&errBuf)),
	)
	if handle == 0 {
		return nil, consumeErr("open rust engine", errBuf)
	}

	return &Engine{
		handle: handle,
		opts:   opts,
	}, nil
}

func (e *Engine) Put(key, value []byte) error {
	if e == nil || e.handle == 0 {
		return errors.New("rust engine is closed")
	}
	var errBuf redigoBuf
	rc, _, _ := api.enginePut.Call(
		e.handle,
		bytesPtr(key),
		uintptr(len(key)),
		bytesPtr(value),
		uintptr(len(value)),
		uintptr(unsafe.Pointer(&errBuf)),
	)
	if int32(rc) != 0 {
		return consumeErr("put rust engine", errBuf)
	}
	return nil
}

func (e *Engine) Get(key []byte) ([]byte, bool, error) {
	if e == nil || e.handle == 0 {
		return nil, false, errors.New("rust engine is closed")
	}
	var valueBuf redigoBuf
	var errBuf redigoBuf
	rc, _, _ := api.engineGet.Call(
		e.handle,
		bytesPtr(key),
		uintptr(len(key)),
		uintptr(unsafe.Pointer(&valueBuf)),
		uintptr(unsafe.Pointer(&errBuf)),
	)
	switch int32(rc) {
	case 1:
		defer freeBuf(valueBuf)
		value := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(valueBuf.Ptr)), int(valueBuf.Len))...)
		return value, true, nil
	case 0:
		return nil, false, nil
	default:
		return nil, false, consumeErr("get rust engine", errBuf)
	}
}

func (e *Engine) Delete(key []byte) error {
	if e == nil || e.handle == 0 {
		return errors.New("rust engine is closed")
	}
	var errBuf redigoBuf
	rc, _, _ := api.engineDelete.Call(
		e.handle,
		bytesPtr(key),
		uintptr(len(key)),
		uintptr(unsafe.Pointer(&errBuf)),
	)
	if int32(rc) != 0 {
		return consumeErr("delete rust engine", errBuf)
	}
	return nil
}

func (e *Engine) CompactUntilStable(maxRounds int) (uint64, error) {
	if e == nil || e.handle == 0 {
		return 0, errors.New("rust engine is closed")
	}
	var rewritten uint64
	var errBuf redigoBuf
	rc, _, _ := api.engineCompact.Call(
		e.handle,
		uintptr(maxRounds),
		uintptr(unsafe.Pointer(&rewritten)),
		uintptr(unsafe.Pointer(&errBuf)),
	)
	if int32(rc) != 0 {
		return 0, consumeErr("compact rust engine", errBuf)
	}
	return rewritten, nil
}

func (e *Engine) Stats() (Stats, error) {
	if e == nil || e.handle == 0 {
		return Stats{}, errors.New("rust engine is closed")
	}
	var out redigoStats
	var errBuf redigoBuf
	rc, _, _ := api.engineStats.Call(
		e.handle,
		uintptr(unsafe.Pointer(&out)),
		uintptr(unsafe.Pointer(&errBuf)),
	)
	if int32(rc) != 0 {
		return Stats{}, consumeErr("stats rust engine", errBuf)
	}
	return Stats{
		Keys:            out.Keys,
		ActiveSegmentID: out.ActiveSegmentID,
		SegmentCount:    out.SegmentCount,
		CachedReaders:   out.CachedReaders,
		PinnedSegments:  out.PinnedSegments,
		PendingReclaims: out.PendingReclaims,
	}, nil
}

func (e *Engine) LoadAllKeys() (map[string][]byte, error) {
	if e == nil || e.handle == 0 {
		return nil, errors.New("rust engine is closed")
	}

	var errBuf redigoBuf
	iterHandle, _, _ := api.engineIterOpen.Call(
		e.handle,
		uintptr(unsafe.Pointer(&errBuf)),
	)
	if iterHandle == 0 {
		return nil, consumeErr("open rust iterator", errBuf)
	}
	defer api.engineIterClose.Call(iterHandle)

	out := make(map[string][]byte)
	for {
		var keyBuf redigoBuf
		var valueBuf redigoBuf
		var nextErr redigoBuf
		rc, _, _ := api.engineIterNext.Call(
			iterHandle,
			uintptr(unsafe.Pointer(&keyBuf)),
			uintptr(unsafe.Pointer(&valueBuf)),
			uintptr(unsafe.Pointer(&nextErr)),
		)
		switch int32(rc) {
		case 1:
			key := string(append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(keyBuf.Ptr)), int(keyBuf.Len))...))
			value := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(valueBuf.Ptr)), int(valueBuf.Len))...)
			freeBuf(keyBuf)
			freeBuf(valueBuf)
			out[key] = value
		case 0:
			return out, nil
		default:
			freeBuf(keyBuf)
			freeBuf(valueBuf)
			return nil, consumeErr("iterate rust engine", nextErr)
		}
	}
}

func (e *Engine) Reset() error {
	if e == nil {
		return nil
	}
	if err := e.Close(); err != nil {
		return err
	}
	if err := os.RemoveAll(e.opts.DataDir); err != nil {
		return fmt.Errorf("remove rust engine dir: %w", err)
	}
	reopened, err := Open(e.opts)
	if err != nil {
		return err
	}
	e.handle = reopened.handle
	reopened.handle = 0
	return nil
}

func (e *Engine) Close() error {
	if e == nil || e.handle == 0 {
		return nil
	}
	var errBuf redigoBuf
	rc, _, _ := api.engineClose.Call(
		e.handle,
		uintptr(unsafe.Pointer(&errBuf)),
	)
	e.handle = 0
	if int32(rc) != 0 {
		return consumeErr("close rust engine", errBuf)
	}
	return nil
}

func ensureDLL() error {
	loadOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			loadErr = err
			return
		}
		engineDir := filepath.Join(root, "engine_v2")
		dllPath := filepath.Join(engineDir, "target", "release", "engine_v2.dll")
		if err := buildDLL(engineDir); err != nil {
			loadErr = err
			return
		}
		api, loadErr = loadDLL(dllPath)
	})
	return loadErr
}

func buildDLL(engineDir string) error {
	cmd := exec.Command("cargo", "build", "--release")
	cmd.Dir = engineDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build rust engine dll: %w", err)
	}
	return nil
}

func loadDLL(path string) (*dllAPI, error) {
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, fmt.Errorf("load rust engine dll: %w", err)
	}

	find := func(name string) (*syscall.Proc, error) {
		proc, procErr := dll.FindProc(name)
		if procErr != nil {
			return nil, fmt.Errorf("find proc %s: %w", name, procErr)
		}
		return proc, nil
	}

	bufFree, err := find("redigo_buf_free")
	if err != nil {
		return nil, err
	}
	engineOpen, err := find("redigo_engine_open")
	if err != nil {
		return nil, err
	}
	engineClose, err := find("redigo_engine_close")
	if err != nil {
		return nil, err
	}
	enginePut, err := find("redigo_engine_put")
	if err != nil {
		return nil, err
	}
	engineGet, err := find("redigo_engine_get")
	if err != nil {
		return nil, err
	}
	engineDelete, err := find("redigo_engine_delete")
	if err != nil {
		return nil, err
	}
	engineCompact, err := find("redigo_engine_compact_until_stable")
	if err != nil {
		return nil, err
	}
	engineStats, err := find("redigo_engine_stats")
	if err != nil {
		return nil, err
	}
	engineIterOpen, err := find("redigo_engine_iter_open")
	if err != nil {
		return nil, err
	}
	engineIterNext, err := find("redigo_engine_iter_next")
	if err != nil {
		return nil, err
	}
	engineIterClose, err := find("redigo_engine_iter_close")
	if err != nil {
		return nil, err
	}

	return &dllAPI{
		dll:             dll,
		bufFree:         bufFree,
		engineOpen:      engineOpen,
		engineClose:     engineClose,
		enginePut:       enginePut,
		engineGet:       engineGet,
		engineDelete:    engineDelete,
		engineCompact:   engineCompact,
		engineStats:     engineStats,
		engineIterOpen:  engineIterOpen,
		engineIterNext:  engineIterNext,
		engineIterClose: engineIterClose,
	}, nil
}

func repoRoot() (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve current file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..")), nil
}

func bytesPtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

func freeBuf(buf redigoBuf) {
	if buf.Ptr == 0 || buf.Len == 0 {
		return
	}
	api.bufFree.Call(uintptr(buf.Ptr), uintptr(buf.Len))
}

func consumeErr(prefix string, buf redigoBuf) error {
	if buf.Ptr == 0 || buf.Len == 0 {
		return errors.New(prefix)
	}
	defer freeBuf(buf)
	msg := string(unsafe.Slice((*byte)(unsafe.Pointer(buf.Ptr)), int(buf.Len)))
	return fmt.Errorf("%s: %s", prefix, msg)
}
