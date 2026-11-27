//go:build windows

package app

import "golang.org/x/sys/windows"

// systemConfigDir is the real roaming AppData folder. The app points APPDATA
// at its data directory for the tools it starts, and a relaunch inherits
// that; asking Windows for the known folder keeps the bootstrap path stable.
func systemConfigDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, windows.KF_FLAG_DEFAULT)
}

// systemLocalAppDataDir is the real local AppData folder, not the cache path
// the app points LOCALAPPDATA at.
func systemLocalAppDataDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
}
