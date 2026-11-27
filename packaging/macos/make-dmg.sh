#!/bin/bash
# Builds Trackz.app (the viewer, trackz-viz, as its executable, and the
# recorder, trackz, next to it; ad-hoc signed) and packs it in a disk image
# with a link to /Applications and a read-me, one per architecture. Runs on
# macOS, from the repository root:
#   packaging/macos/make-dmg.sh <version> <output dir> [goarch...]
# The architectures default to arm64 and amd64: make-dmg.sh v1.2.3 dist
# writes dist/Trackz-1.2.3-arm64.dmg and dist/Trackz-1.2.3-x86_64.dmg (named
# like the archives, after `uname -m`). .goreleaser.darwin.yaml runs it.
set -euo pipefail

tag=$1
out=$2
shift 2
archs=("$@")
[ ${#archs[@]} -gt 0 ] || archs=(arm64 amd64)
version=${tag#v}
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$out"

for arch in "${archs[@]}"; do
	case $arch in
	arm64) machine=arm64 ;;
	amd64) machine=x86_64 ;;
	*)
		echo "unsupported architecture: $arch" >&2
		exit 1
		;;
	esac
	root=$work/$arch
	app=$root/Trackz.app
	mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

	# Apple's clang builds either architecture on either Mac. The same
	# flags as .goreleaser.darwin.yaml's builds.
	CGO_ENABLED=1 GOOS=darwin GOARCH=$arch go build -trimpath \
		-ldflags "-s -w -X github.com/solworktech/trackz/internal/version.Version=$tag" \
		-o "$app/Contents/MacOS/" ./cmd/trackz ./cmd/trackz-viz

	sed "s/@VERSION@/$version/g" "$here/Info.plist" >"$app/Contents/Info.plist"
	printf 'APPL????' >"$app/Contents/PkgInfo"
	cp "$here/Trackz.icns" "$app/Contents/Resources/"
	# The LaunchAgent that starts the recorder at login (see README.txt).
	cp "$here/../launchd/org.solworktech.trackz.plist" "$app/Contents/Resources/"
	plutil -lint "$app/Contents/Info.plist" "$app/Contents/Resources/org.solworktech.trackz.plist"

	# Ad-hoc signatures (no Developer ID): the recorder with an identifier
	# of its own (the one Privacy & Security knows it by), then the bundle.
	codesign --force --sign - --identifier org.solworktech.trackz "$app/Contents/MacOS/trackz"
	codesign --force --sign - "$app"
	codesign --verify --strict --verbose=2 "$app"

	sed -e "s/@VERSION@/$version/g" -e "s/@ARCH@/$machine/g" "$here/README.txt" >"$root/Read me.txt"
	ln -s /Applications "$root/Applications"

	dmg=$out/Trackz-$version-$machine.dmg
	# hdiutil sometimes fails with "Resource busy" on CI runners; retry.
	for try in 1 2 3; do
		hdiutil create -volname "Trackz $version" -srcfolder "$root" -fs HFS+ \
			-format UDZO -ov "$dmg" && break
		[ "$try" -eq 3 ] && exit 1
		sleep 5
	done
	echo "$dmg"
done
