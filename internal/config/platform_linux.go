//go:build linux && !android

package config

func detectPlatform() string {
	return "linux"
}
