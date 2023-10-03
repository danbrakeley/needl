//go:build !windows

package main

import (
	"os"
	"time"
)

func modifyFileTime(path string, stamp time.Time) error {
	// this panics on windows if file not found. need to test on other OS's.
	return os.Chtimes(path, time.Time{}, stamp)
}
