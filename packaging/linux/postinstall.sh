#!/bin/sh
# deb postinst and rpm %post: enable the trackz user unit for every user and,
# on a new installation, start it in the graphical sessions already running;
# on an upgrade, restart it where it runs.
#   deb: postinst configure [<old version>] (or abort-*)
#   rpm: %post <installed instances: 1 new installation, 2+ upgrade>
set -u

unit=trackz.service

case "${1:-}" in
configure) if [ -z "${2:-}" ]; then fresh=1; else fresh=0; fi ;;
1) fresh=1 ;;
[0-9]*) fresh=0 ;;
*) exit 0 ;;
esac

command -v systemctl >/dev/null 2>&1 || exit 0

# Only on a new installation, so an upgrade keeps an administrator's
# `systemctl --global disable trackz`. Works without a running systemd.
if [ "$fresh" = 1 ]; then
	systemctl --global enable "$unit" >/dev/null 2>&1 || true
fi

# The user managers of the users logged in now (none in a chroot or a
# container without systemd).
[ -d /run/systemd/system ] || exit 0
command -v loginctl >/dev/null 2>&1 || exit 0
for user in $(loginctl list-users --no-legend 2>/dev/null | awk '{ print $2 }'); do
	m="--machine=$user@.host"
	systemctl --user "$m" daemon-reload >/dev/null 2>&1 || continue
	if [ "$fresh" = 1 ]; then
		# Only in a graphical session (trackz needs its display), not,
		# say, for a user logged in over ssh.
		if systemctl --user "$m" --quiet is-active graphical-session.target 2>/dev/null; then
			systemctl --user "$m" --no-block start "$unit" >/dev/null 2>&1 || true
		fi
	else
		systemctl --user "$m" --no-block try-restart "$unit" >/dev/null 2>&1 || true
	fi
done
exit 0
