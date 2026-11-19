#!/bin/sh
# deb prerm and rpm %preun: on removal (not on an upgrade), stop trackz for
# the users logged in now, which closes their open records, and undo the
# global enable of the user unit.
#   deb: prerm remove | upgrade <new version> | deconfigure | failed-upgrade
#   rpm: %preun <instances left: 0 removal, 1+ upgrade>
set -u

unit=trackz.service

case "${1:-}" in
remove | 0) ;;
*) exit 0 ;;
esac

command -v systemctl >/dev/null 2>&1 || exit 0

if [ -d /run/systemd/system ] && command -v loginctl >/dev/null 2>&1; then
	for user in $(loginctl list-users --no-legend 2>/dev/null | awk '{ print $2 }'); do
		systemctl --user "--machine=$user@.host" stop "$unit" >/dev/null 2>&1 || true
	done
fi
systemctl --global disable "$unit" >/dev/null 2>&1 || true
exit 0
