//go:build !windows

package main

import "fmt"

// installWindowsService is a platform guard stub for non-Windows builds.
func installWindowsService(name, binPath string) error {
	return fmt.Errorf("Windows service install is only supported on Windows")
}

// isWindowsService is always false on non-Windows platforms.
func isWindowsService() (bool, error) {
	return false, nil
}

// runWindowsService returns a descriptive error on unsupported platforms.
func runWindowsService(cfg *Config) error {
	return fmt.Errorf("Windows service runtime is only supported on Windows")
}
