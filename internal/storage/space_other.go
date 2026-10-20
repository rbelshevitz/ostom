//go:build !windows

package storage

import "fmt"

func Query(string) (Space, error) {
	return Space{}, fmt.Errorf("disk space query is not implemented on this platform")
}
