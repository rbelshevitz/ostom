//go:build windows

package storage

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func Query(path string) (Space, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Space{}, err
	}
	name, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return Space{}, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(name, &available, &total, &free); err != nil {
		return Space{}, fmt.Errorf("query disk space: %w", err)
	}
	return Space{Available: available, Total: total, Free: free}, nil
}
