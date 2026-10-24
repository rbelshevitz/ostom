//go:build !windows

package memory

func processWorkingSet() uint64 { return 0 }
