package buffer

import (
	"fmt"
	"sync"

	"github.com/phroun/garland"
)

// HostFS is the minimal host-side file access mew's scaffolding runs on when
// a host virtualizes the file system: whole-file reads and writes. It is a
// structural subset of the editor's FileSystem interface, so any host FS
// satisfies it without importing this package.
type HostFS interface {
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte) error
}

// bridgeFS adapts a HostFS to garland.FileSystemInterface so host-virtualized
// buffers open and save through garland's engine (history preservation,
// scars, save points) instead of bypassing it with raw content writes.
//
// Handles are materialized in memory: a read handle snapshots the file's
// bytes at Open; a write handle accumulates bytes and flushes to the host in
// one WriteFile at Close. Garland's save engine only needs
// Open/SeekByte/ReadBytes/WriteBytes/Truncate/Close/IsEOF, so this covers
// every path it exercises; metadata (Stat/DeviceInfo) is unsupported, which
// garland handles by tracking only volunteered metadata.
type bridgeFS struct {
	host HostFS
}

// BridgeFS wraps a host's file callbacks as a garland file system.
func BridgeFS(host HostFS) garland.FileSystemInterface {
	return &bridgeFS{host: host}
}

// bridgeHandle is an in-memory file handle over host bytes.
type bridgeHandle struct {
	fs    *bridgeFS
	name  string
	mu    sync.Mutex
	data  []byte
	pos   int64
	write bool // flush to host on Close
}

func (f *bridgeFS) Open(name string, mode garland.OpenMode) (garland.FileHandle, error) {
	h := &bridgeHandle{fs: f, name: name}
	switch mode {
	case garland.OpenModeRead:
		data, err := f.host.ReadFile(name)
		if err != nil {
			return nil, err
		}
		h.data = data
	case garland.OpenModeWrite:
		// Fresh write: start empty (garland truncates anyway).
		h.write = true
	case garland.OpenModeReadWrite:
		// In-place save: seed with current content when the file exists so
		// unwritten warm spans keep their bytes, flush the whole image back.
		if data, err := f.host.ReadFile(name); err == nil {
			h.data = append([]byte(nil), data...)
		}
		h.write = true
	default:
		return nil, fmt.Errorf("unknown open mode %d", mode)
	}
	return h, nil
}

func (f *bridgeFS) handle(h garland.FileHandle) (*bridgeHandle, error) {
	bh, ok := h.(*bridgeHandle)
	if !ok || bh == nil {
		return nil, fmt.Errorf("foreign file handle")
	}
	return bh, nil
}

func (f *bridgeFS) SeekByte(h garland.FileHandle, pos int64) error {
	bh, err := f.handle(h)
	if err != nil {
		return err
	}
	bh.mu.Lock()
	defer bh.mu.Unlock()
	if pos < 0 {
		return fmt.Errorf("negative seek")
	}
	bh.pos = pos
	return nil
}

func (f *bridgeFS) ReadBytes(h garland.FileHandle, length int) ([]byte, error) {
	bh, err := f.handle(h)
	if err != nil {
		return nil, err
	}
	bh.mu.Lock()
	defer bh.mu.Unlock()
	if bh.pos >= int64(len(bh.data)) {
		return nil, nil
	}
	end := bh.pos + int64(length)
	if end > int64(len(bh.data)) {
		end = int64(len(bh.data))
	}
	out := append([]byte(nil), bh.data[bh.pos:end]...)
	bh.pos = end
	return out, nil
}

func (f *bridgeFS) IsEOF(h garland.FileHandle) bool {
	bh, err := f.handle(h)
	if err != nil {
		return true
	}
	bh.mu.Lock()
	defer bh.mu.Unlock()
	return bh.pos >= int64(len(bh.data))
}

func (f *bridgeFS) Close(h garland.FileHandle) error {
	bh, err := f.handle(h)
	if err != nil {
		return err
	}
	bh.mu.Lock()
	defer bh.mu.Unlock()
	if bh.write {
		bh.write = false
		return f.host.WriteFile(bh.name, bh.data)
	}
	return nil
}

func (f *bridgeFS) HasChanged(h garland.FileHandle) (bool, error) {
	return false, garland.ErrNotSupported
}

func (f *bridgeFS) FileSize(h garland.FileHandle) (int64, error) {
	bh, err := f.handle(h)
	if err != nil {
		return 0, err
	}
	bh.mu.Lock()
	defer bh.mu.Unlock()
	return int64(len(bh.data)), nil
}

func (f *bridgeFS) BlockChecksum(h garland.FileHandle, start, length int64) ([]byte, error) {
	return nil, garland.ErrNotSupported
}

func (f *bridgeFS) WriteBytes(h garland.FileHandle, data []byte) error {
	bh, err := f.handle(h)
	if err != nil {
		return err
	}
	bh.mu.Lock()
	defer bh.mu.Unlock()
	if !bh.write {
		return fmt.Errorf("handle not open for writing")
	}
	end := bh.pos + int64(len(data))
	if end > int64(len(bh.data)) {
		grown := make([]byte, end)
		copy(grown, bh.data)
		bh.data = grown
	}
	copy(bh.data[bh.pos:end], data)
	bh.pos = end
	return nil
}

func (f *bridgeFS) Truncate(h garland.FileHandle, size int64) error {
	bh, err := f.handle(h)
	if err != nil {
		return err
	}
	bh.mu.Lock()
	defer bh.mu.Unlock()
	if !bh.write {
		return fmt.Errorf("handle not open for writing")
	}
	if size < 0 {
		return fmt.Errorf("negative truncate")
	}
	if size <= int64(len(bh.data)) {
		bh.data = bh.data[:size]
	} else {
		grown := make([]byte, size)
		copy(grown, bh.data)
		bh.data = grown
	}
	if bh.pos > size {
		bh.pos = size
	}
	return nil
}

func (f *bridgeFS) WriteFile(name string, data []byte) error {
	return f.host.WriteFile(name, data)
}

func (f *bridgeFS) ReadFile(name string) ([]byte, error) {
	return f.host.ReadFile(name)
}

// Directory operations: hosts expose flat read/write callbacks only. MkdirAll
// succeeds as a no-op (hosts create paths implicitly on WriteFile); the
// destructive operations are refused.
func (f *bridgeFS) MkdirAll(path string) error { return nil }

func (f *bridgeFS) Remove(name string) error { return garland.ErrNotSupported }

func (f *bridgeFS) Rmdir(path string) error { return garland.ErrNotSupported }

func (f *bridgeFS) Rename(oldpath, newpath string) error { return garland.ErrNotSupported }

// Stat is unsupported: garland then tracks only metadata the application
// volunteers through ReportSourceMetadata (per its documented contract a
// missing-metadata FS is valid).
func (f *bridgeFS) Stat(name string) (garland.FileMetadata, error) {
	return garland.FileMetadata{}, garland.ErrNotSupported
}

func (f *bridgeFS) DeviceInfo(name string) (garland.DeviceInfo, error) {
	return garland.DeviceInfo{}, garland.ErrNotSupported
}
