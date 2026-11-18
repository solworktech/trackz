//go:build !(linux || freebsd || openbsd || netbsd || dragonfly || darwin || windows)

package systheme

import "context"

func detect() Theme                                          { return Unknown }
func subscribe(ctx context.Context, changes chan<- struct{}) {}
