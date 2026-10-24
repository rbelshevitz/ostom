//go:build windows

package memory

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var processMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

func processWorkingSet() uint64 {
	counters := processMemoryCounters{cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	handle, err := windows.GetCurrentProcess()
	if err != nil {
		return 0
	}
	result, _, _ := processMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(unsafe.Sizeof(counters)),
	)
	if result == 0 {
		return 0
	}
	return uint64(counters.WorkingSetSize)
}
