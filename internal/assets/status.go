package assets

import (
	"bytes"
	"embed"
	"image"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

//go:embed status/*.svg
var statusFiles embed.FS

// statusKinds maps words in a status to its icon, checked in order; a
// status matching none gets the generic icon (an hourglass).
var statusKinds = []struct {
	icon  string
	words []string
}{
	{"lunch", []string{"lunch", "dinner", "breakfast", "food", "eat"}},
	{"coffee", []string{"coffee", "tea", "break"}},
	{"meeting", []string{"meeting", "call with", "standup", "stand-up", "interview"}},
	{"phone", []string{"phone", "call"}},
	{"errand", []string{"errand", "shop", "groceries", "school run"}},
	{"away", []string{"away", "out", "afk"}},
}

// StatusIconName returns the name of the icon for a status ("Out to
// lunch" → "lunch"); "status" for the generic one.
func StatusIconName(status string) string {
	s := strings.ToLower(status)
	for _, k := range statusKinds {
		for _, w := range k.words {
			if strings.Contains(s, w) {
				return k.icon
			}
		}
	}
	return "status"
}

// StatusIcon renders the icon for a status (see StatusIconName) at size
// pixels, or returns nil if it cannot be drawn.
func StatusIcon(status string, size int) image.Image {
	return statusIconByName(StatusIconName(status), size)
}

func statusIconByName(name string, size int) image.Image {
	data, err := statusFiles.ReadFile("status/" + name + ".svg")
	if err != nil {
		return nil
	}
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.StrictErrorMode)
	if err != nil {
		return nil
	}
	icon.SetTarget(0, 0, float64(size), float64(size))
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	icon.Draw(rasterx.NewDasher(size, size, rasterx.NewScannerGV(size, size, img, img.Bounds())), 1)
	return img
}
