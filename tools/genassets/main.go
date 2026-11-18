// Command genassets renders internal/assets/logo.svg into the icon files the
// programs and packaging use. Run it from the repository root after
// changing the logo:
//
//	go run ./tools/genassets   (from tools/genassets: go run . -root ../..)
//
// It writes:
//
//	internal/assets/icons/trackz-<size>.png    embedded in trackz-viz
//	packaging/icons/hicolor/<size>/apps/trackz.png, .../scalable/apps/trackz.svg
//	packaging/windows/trackz.ico
//	cmd/trackz-viz/rsrc_windows_<arch>.syso   Windows window/taskbar icon
//	packaging/windows/wix/{banner,dialog}.bmp  installer images
//	packaging/windows/wix/License.rtf          LICENSE for the installer
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"github.com/tc-hib/winres"
)

var sizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	svgPath := filepath.Join(*root, "internal", "assets", "logo.svg")
	svg, err := os.ReadFile(svgPath)
	check(err)

	imgs := map[int]image.Image{}
	for _, s := range sizes {
		imgs[s] = render(svg, s)
		writePNG(filepath.Join(*root, "internal", "assets", "icons", fmt.Sprintf("trackz-%d.png", s)), imgs[s])
		writePNG(filepath.Join(*root, "packaging", "icons", "hicolor", fmt.Sprintf("%dx%d", s, s), "apps", "trackz.png"), imgs[s])
	}
	scalable := filepath.Join(*root, "packaging", "icons", "hicolor", "scalable", "apps", "trackz.svg")
	check(os.MkdirAll(filepath.Dir(scalable), 0o755))
	check(os.WriteFile(scalable, svg, 0o644))

	var list []image.Image
	for _, s := range []int{16, 24, 32, 48, 64, 128, 256} {
		list = append(list, imgs[s])
	}
	ico, err := winres.NewIconFromImages(list)
	check(err)
	var buf bytes.Buffer
	check(ico.SaveICO(&buf))
	icoPath := filepath.Join(*root, "packaging", "windows", "trackz.ico")
	check(os.MkdirAll(filepath.Dir(icoPath), 0o755))
	check(os.WriteFile(icoPath, buf.Bytes(), 0o644))

	// Gio uses the icon with resource ID 1 (or IDI_APPLICATION) for its
	// windows; Explorer shows the first icon group.
	rs := winres.ResourceSet{}
	check(rs.SetIcon(winres.ID(1), ico))
	for _, arch := range []winres.Arch{winres.ArchAMD64, winres.ArchARM64, winres.ArchI386} {
		p := filepath.Join(*root, "cmd", "trackz-viz", fmt.Sprintf("rsrc_windows_%s.syso", arch))
		f, err := os.Create(p)
		check(err)
		check(rs.WriteObject(f, arch))
		check(f.Close())
	}
	writeInstallerAssets(*root, svg)
	log.Printf("icons and installer images generated from %s", svgPath)
}

func render(svg []byte, size int) image.Image {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svg), oksvg.StrictErrorMode)
	check(err)
	icon.SetTarget(0, 0, float64(size), float64(size))
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	icon.Draw(rasterx.NewDasher(size, size, rasterx.NewScannerGV(size, size, img, img.Bounds())), 1)
	return img
}

func writePNG(path string, img image.Image) {
	check(os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	check(err)
	check(png.Encode(f, img))
	check(f.Close())
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
