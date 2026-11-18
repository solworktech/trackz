// Package assets embeds the Trackz logo, rendered from logo.svg by
// tools/genassets.
package assets

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/png"
)

//go:embed icons/*.png
var files embed.FS

// Sizes lists the pixel sizes of the embedded PNG icons.
var Sizes = []int{16, 24, 32, 48, 64, 128, 256}

// Icon returns the logo at the embedded size closest to (and not below)
// size, or the largest one.
func Icon(size int) image.Image {
	pick := Sizes[len(Sizes)-1]
	for _, s := range Sizes {
		if s >= size {
			pick = s
			break
		}
	}
	data, err := files.ReadFile(fmt.Sprintf("icons/trackz-%d.png", pick))
	if err != nil {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

// IconPNG returns the PNG bytes of the logo at an embedded size.
func IconPNG(size int) []byte {
	b, err := files.ReadFile(fmt.Sprintf("icons/trackz-%d.png", size))
	if err != nil {
		return nil // not an embedded size
	}
	return b
}
