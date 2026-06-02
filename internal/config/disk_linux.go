//go:build linux

package config

import "syscall"

type DiskStats struct {
	TotalBytes     int64
	AvailableBytes int64
}

func GetDiskStats(path string) (*DiskStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}
	total := int64(stat.Bsize) * int64(stat.Blocks)
	avail := int64(stat.Bsize) * int64(stat.Bavail)
	return &DiskStats{TotalBytes: total, AvailableBytes: avail}, nil
}
