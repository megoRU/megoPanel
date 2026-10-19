#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Run this uninstaller as root: sudo scripts/uninstall-debian.sh" >&2
  exit 1
fi

confirm() {
  local prompt="$1"
  local answer=""
  read -r -p "${prompt} [y/N]: " answer
  [[ "${answer}" =~ ^[Yy]$ ]]
}

echo "MegoPanel removal"
echo "================="
echo "This removes the MegoPanel application and its systemd unit."
echo "Nginx, MariaDB, phpMyAdmin, websites, and their data are shared server components and will be left in place."

if ! confirm "Remove the MegoPanel application?"; then
  echo "Cancelled."
  exit 0
fi

echo ""
echo "Panel configuration and SQLite data are in /etc/megopanel and /var/lib/megopanel."
echo "Backups in /var/backups/megopanel are kept even when panel data is removed."
echo "They can contain administrator and database credentials. They will be preserved by default."
remove_panel_data=false
read -r -p "To permanently delete those directories, type DELETE: " data_confirmation
if [[ "${data_confirmation}" == "DELETE" ]]; then
  remove_panel_data=true
fi

systemctl disable --now megopanel 2>/dev/null || true
rm -f /etc/systemd/system/megopanel.service /usr/local/bin/megopanel
systemctl daemon-reload
systemctl reset-failed megopanel 2>/dev/null || true
rm -rf /opt/megopanel

if [[ "${remove_panel_data}" == "true" ]]; then
  rm -rf /etc/megopanel /var/lib/megopanel
  echo "Removed MegoPanel configuration and data."
else
  echo "Preserved MegoPanel configuration and data in /etc/megopanel and /var/lib/megopanel."
fi

echo "MegoPanel application removed. Shared server components and their data were preserved."
