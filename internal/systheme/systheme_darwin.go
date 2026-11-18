//go:build darwin

package systheme

import (
	"context"
	"os/exec"
	"strings"
)

// detect reads AppleInterfaceStyle, which is "Dark" in dark mode and unset
// (the command fails) in light mode.
func detect() Theme {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	if err != nil {
		return Light
	}
	if strings.Contains(strings.ToLower(string(out)), "dark") {
		return Dark
	}
	return Light
}

func subscribe(ctx context.Context, changes chan<- struct{}) {} // polled
