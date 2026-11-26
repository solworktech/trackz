#!/bin/bash
# Grants Accessibility (window titles) and Screen Recording (screenshots,
# the recording) to the given programs (paths) and apps (bundle:<id>), and
# to the shells and runner processes the smoke test runs them from, by
# writing the TCC databases: nobody is there to answer the permission
# prompts on a CI runner. Best effort: it says what it could not do, and
# the test then shows the effect (no titles, blank screenshots).
#   grant-permissions.sh /path/to/program bundle:org.example.App ...
set -u

clients=("$@" /bin/bash /bin/sh /bin/zsh /usr/sbin/screencapture /usr/bin/osascript)
# The processes GitHub's runner starts the job's steps from, if present:
# macOS may hold them responsible for what the shells do.
for p in /usr/local/opt/runner/provisioner/provisioner \
	"$HOME"/actions-runner/Runner.Listener "$HOME"/actions-runner/bin/Runner.Listener \
	"$HOME"/actions-runner/bin/Runner.Worker; do
	[ -e "$p" ] && clients+=("$p")
done

grant() { # db service client client_type
	sudo sqlite3 "$1" "INSERT OR REPLACE INTO access
		(service, client, client_type, auth_value, auth_reason, auth_version, flags, last_modified)
		VALUES ('$2', '$3', $4, 2, 4, 1, 0, CAST(strftime('%s','now') AS INTEGER));" 2>&1
}

status=0
system_db="/Library/Application Support/com.apple.TCC/TCC.db"
user_db="$HOME/Library/Application Support/com.apple.TCC/TCC.db"
for c in "${clients[@]}"; do
	client=$c type=1 # a path
	if [[ $c == bundle:* ]]; then
		client=${c#bundle:} type=0 # a bundle identifier
	fi
	for svc in kTCCServiceAccessibility kTCCServiceScreenCapture kTCCServicePostEvent; do
		for db in "$system_db" "$user_db"; do
			[ -e "$db" ] || continue
			if out=$(grant "$db" "$svc" "$client" "$type") && [ -z "$out" ]; then
				echo "granted $svc to $client ($db)"
			else
				echo "could not grant $svc to $client in $db: $out"
				status=1
			fi
		done
	done
done
# tccd caches decisions; launchd restarts it.
sudo pkill -9 tccd 2>/dev/null || true
sleep 2
echo "SIP: $(csrutil status 2>&1)"
exit "$status"
