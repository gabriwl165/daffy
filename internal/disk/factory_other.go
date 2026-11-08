//go:build !linux

package disk

func newScanner() Scanner { return &scanner{platform: unsupportedPlatform{}} }
