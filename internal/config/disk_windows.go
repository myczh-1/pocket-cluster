//go:build windows

package config

type DiskStats struct {
	TotalBytes     int64
	AvailableBytes int64
}

func GetDiskStats(path string) (*DiskStats, error) {
	// TODO: implement Windows disk stats via GetDiskFreeSpaceEx
	return &DiskStats{}, nil
}
