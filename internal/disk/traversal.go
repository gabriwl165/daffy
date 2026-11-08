package disk

import "os"

type EntryMetadata struct {
	ApparentBytes  int64
	AllocatedBytes int64
	Identity       FileIdentity
	Links          uint64
	Mode           os.FileMode
}

type Directory interface {
	ReadDir(n int) ([]os.DirEntry, error)
	Stat() (os.FileInfo, error)
	Close() error
	ChildMetadata(name string) (EntryMetadata, error)
	OpenChild(name string) (Directory, error)
}
