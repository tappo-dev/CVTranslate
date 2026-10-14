//go:build !windows

package main

import "os"

func restrictDirectory(path string) error {
	return os.Chmod(path, 0700)
}
