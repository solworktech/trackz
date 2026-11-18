//go:build windows

package systheme

import (
	"context"

	"golang.org/x/sys/windows/registry"
)

// detect reads AppsUseLightTheme (0 = dark, 1 = light).
func detect() Theme {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return Unknown
	}
	defer func() { _ = k.Close() }() // opened for reading
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return Unknown
	}
	if v == 0 {
		return Dark
	}
	return Light
}

func subscribe(ctx context.Context, changes chan<- struct{}) {} // polled
