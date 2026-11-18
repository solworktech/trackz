package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/bmp"
)

// writeInstallerAssets writes what the WiX installer shows: the banner at
// the top of most dialogs (493×58), the side image of the first and last
// dialogs (493×312, the image on the left 164 px), and the licence as RTF.
func writeInstallerAssets(root string, svg []byte) {
	dir := filepath.Join(root, "packaging", "windows", "wix")
	check(os.MkdirAll(dir, 0o755))

	white := image.NewUniform(color.White)
	tint := image.NewUniform(color.NRGBA{0xee, 0xf0, 0xfa, 0xff})

	banner := image.NewRGBA(image.Rect(0, 0, 493, 58))
	draw.Draw(banner, banner.Bounds(), white, image.Point{}, draw.Src)
	logo := render(svg, 44)
	draw.Draw(banner, image.Rect(493-44-10, 7, 493-10, 51), logo, image.Point{}, draw.Over)
	writeBMP(filepath.Join(dir, "banner.bmp"), banner)

	side := image.NewRGBA(image.Rect(0, 0, 493, 312))
	draw.Draw(side, side.Bounds(), white, image.Point{}, draw.Src)
	draw.Draw(side, image.Rect(0, 0, 164, 312), tint, image.Point{}, draw.Src)
	big := render(svg, 112)
	draw.Draw(side, image.Rect(26, 60, 26+112, 60+112), big, image.Point{}, draw.Over)
	writeBMP(filepath.Join(dir, "dialog.bmp"), side)

	license, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	check(err)
	check(os.WriteFile(filepath.Join(dir, "License.rtf"), []byte(rtf(string(license))), 0o644))
}

func writeBMP(path string, img image.Image) {
	var buf bytes.Buffer
	check(bmp.Encode(&buf, img))
	check(os.WriteFile(path, buf.Bytes(), 0o644))
}

// rtf converts plain text to RTF for a WiX ScrollableText control. Lines of
// a paragraph (separated by blank lines) are joined, so the text wraps to
// the control's width rather than at the file's 70-odd columns.
func rtf(text string) string {
	var b strings.Builder
	b.WriteString(`{\rtf1\ansi\ansicpg1252\deff0{\fonttbl{\f0\fswiss Tahoma;}}\viewkind4\uc1\pard\f0\fs16 `)
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		var lines []string
		for _, l := range strings.Split(para, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			continue
		}
		b.WriteString(rtfEscape(strings.Join(lines, " ")))
		b.WriteString(`\par\par` + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func rtfEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '{' || r == '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x80:
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, `\u%d?`, int16(r))
		}
	}
	return b.String()
}
