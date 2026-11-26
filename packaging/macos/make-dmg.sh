#!/bin/bash
# Builds Trackz.app (the viewer, trackz-viz, as its executable, and the
# recorder, trackz, next to it; universal arm64 + x86_64 binaries, ad-hoc
# signed) and packs it in a disk image with a link to /Applications and a
# read-me. Runs on macOS, from the repository root:
#   packaging/macos/make-dmg.sh <version> <output dir>
# e.g. make-dmg.sh v1.2.3 dist writes dist/Trackz-1.2.3.dmg.
set -euo pipefail

tag=$1
out=$2
version=${tag#v}
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

app=$work/dmg/Trackz.app
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$out"

# Apple's clang builds either architecture on either Mac.
for arch in arm64 amd64; do
	CGO_ENABLED=1 GOOS=darwin GOARCH=$arch go build -trimpath \
		-ldflags "-s -w -X github.com/solworktech/trackz/internal/version.Version=$tag" \
		-o "$work/$arch/" ./cmd/trackz ./cmd/trackz-viz
done
for bin in trackz trackz-viz; do
	lipo -create -output "$app/Contents/MacOS/$bin" "$work/arm64/$bin" "$work/amd64/$bin"
done

sed "s/@VERSION@/$version/g" "$here/Info.plist" >"$app/Contents/Info.plist"
printf 'APPL????' >"$app/Contents/PkgInfo"
cp "$here/Trackz.icns" "$app/Contents/Resources/"
# The LaunchAgent that starts the recorder at login (see README.txt).
cp "$here/../launchd/org.solworktech.trackz.plist" "$app/Contents/Resources/"
plutil -lint "$app/Contents/Info.plist" "$app/Contents/Resources/org.solworktech.trackz.plist"

# Ad-hoc signatures (no Developer ID): the recorder with an identifier of
# its own (the one Privacy & Security knows it by), then the bundle.
codesign --force --sign - --identifier org.solworktech.trackz "$app/Contents/MacOS/trackz"
codesign --force --sign - "$app"
codesign --verify --strict --verbose=2 "$app"

sed "s/@VERSION@/$version/g" "$here/README.txt" >"$work/dmg/Read me.txt"
ln -s /Applications "$work/dmg/Applications"

dmg=$out/Trackz-$version.dmg
# hdiutil sometimes fails with "Resource busy" on CI runners; retry.
for try in 1 2 3; do
	hdiutil create -volname "Trackz $version" -srcfolder "$work/dmg" -fs HFS+ \
		-format UDZO -ov "$dmg" && break
	[ "$try" -eq 3 ] && exit 1
	sleep 5
done
echo "$dmg"
