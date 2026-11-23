//go:build !windows

package completion

func readRegisteredWSLDistros() []string {
	return nil
}
