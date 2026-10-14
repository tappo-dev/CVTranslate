//go:build !(windows && (amd64 || arm64)) && !(darwin && cgo)

package platform

func registerNativeCopySequence(func(copyTrigger, uint64)) (copySequenceBinding, error) {
	return nil, ErrUnsupported
}
