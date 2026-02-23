//go:build !darwin

package vm

// watchOpenURL is a no-op on non-darwin platforms.
func watchOpenURL(done <-chan struct{}, bootstrapDir string) {}
