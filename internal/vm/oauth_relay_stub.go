//go:build !darwin

package vm

func parseOAuthRedirect(rawURL string) (string, bool) { return "", false }
func startOAuthRelay(done <-chan struct{}, bootstrapDir string, port string) error { return nil }
