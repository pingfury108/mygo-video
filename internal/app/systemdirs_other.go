//go:build !windows

package app

import "os"

func systemConfigDir() (string, error) {
	return os.UserConfigDir()
}

func systemLocalAppDataDir() (string, error) {
	return os.UserCacheDir()
}
