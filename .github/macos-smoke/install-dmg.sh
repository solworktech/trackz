#!/bin/bash
# Installs Trackz from a disk image as a user would: mounts it, copies
# Trackz.app to /Applications, unmounts it, and checks the copy (bundle,
# signature, universal binaries, version).
#   install-dmg.sh Trackz-x.y.z.dmg
set -euo pipefail

dmg=$1
mnt=$(mktemp -d)
hdiutil attach -nobrowse -readonly -mountpoint "$mnt" "$dmg"
ls -la "$mnt"
cat "$mnt/Read me.txt"
rm -rf /Applications/Trackz.app
cp -R "$mnt/Trackz.app" /Applications/
hdiutil detach "$mnt"

app=/Applications/Trackz.app
codesign --verify --strict --verbose=2 "$app"
codesign -dv "$app/Contents/MacOS/trackz" 2>&1 | grep -E 'Identifier|Signature'
plutil -p "$app/Contents/Info.plist"
for bin in trackz trackz-viz; do
	lipo -info "$app/Contents/MacOS/$bin"
done
"$app/Contents/MacOS/trackz" version
