package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"
)

// icnsTypes are the PNG entries of a macOS .icns file, by pixel size: the
// 16, 32, 128, 256 and 512 point icons and their @2x versions (types
// supported since OS X 10.7/10.8).
var icnsTypes = []struct {
	typ  string
	size int
}{
	{"icp4", 16}, {"ic11", 32}, // 16pt, @2x
	{"icp5", 32}, {"ic12", 64}, // 32pt, @2x
	{"ic07", 128}, {"ic13", 256}, // 128pt, @2x
	{"ic08", 256}, {"ic14", 512}, // 256pt, @2x
	{"ic09", 512}, {"ic10", 1024}, // 512pt, @2x
}

// writeICNS renders svg into the macOS application icon at path: an "icns"
// header, then for each entry its type, its length (with the 8-byte entry
// header) and the PNG data, big-endian.
func writeICNS(path string, svg []byte) {
	var body bytes.Buffer
	for _, t := range icnsTypes {
		var img bytes.Buffer
		check(png.Encode(&img, render(svg, t.size)))
		body.WriteString(t.typ)
		check(binary.Write(&body, binary.BigEndian, uint32(8+img.Len())))
		body.Write(img.Bytes())
	}
	var out bytes.Buffer
	out.WriteString("icns")
	check(binary.Write(&out, binary.BigEndian, uint32(8+body.Len())))
	out.Write(body.Bytes())
	check(os.MkdirAll(filepath.Dir(path), 0o755))
	check(os.WriteFile(path, out.Bytes(), 0o644))
}
