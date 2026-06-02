//go:build windows

package config

import (
	"math"
	"path/filepath"
	"syscall"
	"unsafe"
)

type DiskStats struct {
	TotalBytes     int64
	AvailableBytes int64
}

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

func GetDiskStats(path string) (*DiskStats, error) {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	pathPtr, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return nil, err
	}
	var available uint64
	var total uint64
	ret, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&available)),
		uintptr(unsafe.Pointer(&total)),
		0,
	)
	if ret == 0 {
		if callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, syscall.EINVAL
	}
	return &DiskStats{
		TotalBytes:     uint64ToInt64(total),
		AvailableBytes: uint64ToInt64(available),
	}, nil
}

func uint64ToInt64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}
