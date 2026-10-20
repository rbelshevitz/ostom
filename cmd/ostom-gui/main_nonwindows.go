//go:build !windows

package main

import "fmt"

// The recovery engine and CLI are cross-platform. The compact native GUI is
// intentionally Windows-only for now; this stub keeps direct package builds
// explicit on Linux and macOS instead of silently producing no package.
func main() {
	fmt.Println("ostom-gui: native GUI is currently available on Windows; use the ostom CLI on this platform")
}
