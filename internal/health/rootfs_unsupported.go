//go:build !linux && !darwin

package health

func coreRootFilesystemFinding() Finding {
	return Finding{ID: coreFindingDiskRoot, Area: AreaDisk, Severity: SeverityUnknown, Summary: "Root filesystem capacity unsupported on this platform"}
}
