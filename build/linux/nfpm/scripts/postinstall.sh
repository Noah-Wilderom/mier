#!/bin/sh
# Desktop menus discover the installed .desktop file and icon automatically.
# Refresh the optional cache without making a missing cache tool fatal.
if [ -f /etc/apparmor.d/mier ] && command -v apparmor_parser >/dev/null 2>&1 && \
   [ -w /sys/kernel/security/apparmor/.load ]; then
  apparmor_parser -r /etc/apparmor.d/mier || exit 1
fi
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications || true
fi
exit 0
