#!/bin/bash
# The macOS smoke test (see .github/workflows/macos-smoke.yml), with Trackz
# installed from the disk image: starts the recorder with the app's
# LaunchAgent, moves focus between apps with known window titles, opens the
# viewer as a user would (open -a Trackz), and leaves everything in $OUT
# for inspection: trackz.db and its records as text, logs, a screenshot
# per step and a screen recording. Fails if the apps do not show up in the
# database.
#   run.sh /Applications/Trackz.app $OUT
set -euo pipefail

APP=$1
OUT=$2
mkdir -p "$OUT/shots" "$OUT/files"
# Where trackz and trackz-viz put the database by default on macOS.
DB="$HOME/Library/Application Support/trackz/trackz.db"
AGENT=org.solworktech.trackz
PLIST="$HOME/Library/LaunchAgents/$AGENT.plist"
DOMAIN="gui/$(id -u)"
step=0

shot() { # name: a screenshot of the whole screen
	step=$((step + 1))
	screencapture -x -C "$OUT/shots/$(printf %02d "$step")-$1.png" || echo "screenshot $1 failed"
}
focus() { # seconds name open-args...: brings an app forward, waits, screenshots
	local wait=$1 name=$2
	shift 2
	echo "== $name: open $*"
	open "$@" || echo "::warning::open $* failed"
	sleep "$wait"
	shot "$name"
}

# Documents with known titles.
printf 'Alpha notes for the trackz smoke test.\n' >"$OUT/files/notes-alpha.txt"
printf 'Beta notes for the trackz smoke test.\n' >"$OUT/files/notes-beta.txt"
cat >"$OUT/files/page.html" <<'HTML'
<!doctype html><title>trackz smoke page</title><h1>trackz smoke page</h1>
HTML

echo "macOS $(sw_vers -productVersion) ($(uname -m))"
system_profiler SPDisplaysDataType 2>/dev/null | sed -n '1,25p' || true

# A recording of the whole run, as well as the screenshots (recordings have
# come out blank on CI before).
screencapture -v -C -V 240 "$OUT/screen.mov" >"$OUT/screencapture.log" 2>&1 &
rec=$!
sleep 2
shot start

# The recorder, from the LaunchAgent the app carries (with -debug added, to
# log what it records and whether it may read window titles).
mkdir -p "$(dirname "$PLIST")"
cp "$APP/Contents/Resources/$AGENT.plist" "$PLIST"
/usr/libexec/PlistBuddy -c "Add :ProgramArguments: string -debug" "$PLIST"
plutil -p "$PLIST"
if launchctl bootstrap "$DOMAIN" "$PLIST"; then
	echo "LaunchAgent loaded"
else
	echo "::warning::launchctl bootstrap failed; starting trackz directly"
	"$APP/Contents/MacOS/trackz" run -debug >/tmp/trackz.log 2>&1 &
fi
sleep 3
launchctl print "$DOMAIN/$AGENT" | sed -n '1,30p' || true
shot trackz-started

focus 5 textedit-alpha -a TextEdit "$OUT/files/notes-alpha.txt"
focus 5 textedit-beta -a TextEdit "$OUT/files/notes-beta.txt"
focus 5 preview -a Preview "$OUT/shots/01-start.png"
focus 8 safari -a Safari "$OUT/files/page.html"
focus 5 terminal -a Terminal
focus 5 finder -a Finder "$OUT/files"
focus 5 textedit-alpha-again -a TextEdit "$OUT/files/notes-alpha.txt"
focus 5 safari-again -a Safari

# The viewer, as a user opens it, while trackz still records (or it would
# warn that it does not): the dashboard of today, which holds the run so far.
focus 10 trackz-viz -a "$APP"
sleep 5
shot trackz-viz-later
"$APP/Contents/MacOS/trackz-viz" -quit || echo "trackz-viz -quit failed"

# Stop the recorder as logging out would (it closes the open record).
launchctl bootout "$DOMAIN/$AGENT" || "$APP/Contents/MacOS/trackz" stop || echo "stopping trackz failed"
sleep 2
shot end
kill -INT "$rec" 2>/dev/null || true
wait "$rec" 2>/dev/null || true

cp /tmp/trackz.log "$OUT/trackz.log" 2>/dev/null || echo "no /tmp/trackz.log"
cp "$DB" "$OUT/trackz.db"
sqlite3 -header -column "$DB" "SELECT id, process_name, app_name, window_name,
	datetime(focus_start_time, 'unixepoch') AS start, focus_end_time - focus_start_time AS seconds
	FROM trackz ORDER BY id" >"$OUT/records.txt"
cat "$OUT/records.txt"

# The apps must show up; their titles only with Accessibility, so a missing
# title is reported, not failed on (trackz.log says whether it was trusted).
fail=0
for app in TextEdit Preview Safari Terminal Finder Trackz; do
	n=$(sqlite3 "$DB" "SELECT COUNT(*) FROM trackz WHERE process_name LIKE '$app%' OR app_name = '$app'")
	if [ "$n" -eq 0 ]; then
		echo "::error::no records for $app"
		fail=1
	fi
done
for title in notes-alpha.txt notes-beta.txt 'trackz smoke page'; do
	n=$(sqlite3 "$DB" "SELECT COUNT(*) FROM trackz WHERE window_name LIKE '%$title%'")
	if [ "$n" -eq 0 ]; then
		echo "::warning::no record with the title \"$title\" (Accessibility not granted?)"
	fi
done
exit "$fail"
