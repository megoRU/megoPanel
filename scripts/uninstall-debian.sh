#!/usr/bin/env bash
set -euo pipefail

# Ensure run as root
if [[ "${EUID}" -ne 0 ]]; then
  echo "Run this uninstaller as root: sudo scripts/uninstall-debian.sh" >&2
  exit 1
fi

BLUE="\033[1;34m"
GREEN="\033[1;32m"
RED="\033[1;31m"
BOLD="\033[1m"
RESET="\033[0m"

log_step() {
  echo -e "${BLUE}[•]${RESET} $1"
}

log_success() {
  echo -e "${GREEN}[✓]${RESET} $1"
}

echo -e "${BOLD}MegoPanel Uninstallation Script${RESET}"
echo "========================================="

# Stop and disable megopanel
log_step "Stopping and disabling MegoPanel service..."
systemctl stop megopanel 2>/dev/null || true
systemctl disable megopanel 2>/dev/null || true

log_step "Removing MegoPanel systemd service and binaries..."
rm -f /etc/systemd/system/megopanel.service
systemctl daemon-reload 2>/dev/null || true
systemctl reset-failed 2>/dev/null || true
rm -f /usr/local/bin/megopanel

log_step "Deleting MegoPanel data and configuration files..."
rm -rf /etc/megopanel
rm -rf /var/lib/megopanel
rm -rf /opt/megopanel
rm -rf /root/megopanel
rm -rf ~/megopanel

log_step "Removing megopanel user and group..."
userdel -r megopanel 2>/dev/null || true
groupdel megopanel 2>/dev/null || true

# Uninstall Nginx
log_step "Stopping and purging Nginx..."
systemctl stop nginx 2>/dev/null || true
apt-get purge -y nginx nginx-common nginx-core 2>/dev/null || true
apt-get autoremove --purge -y 2>/dev/null || true
rm -rf /etc/nginx
rm -rf /var/log/nginx
rm -rf /var/www

# Uninstall MariaDB
log_step "Stopping and purging MariaDB database server..."
systemctl stop mariadb 2>/dev/null || true
apt-get purge -y mariadb-server mariadb-client mariadb-common 2>/dev/null || true
apt-get autoremove --purge -y 2>/dev/null || true
rm -rf /etc/mysql
rm -rf /var/lib/mysql
rm -rf /var/log/mysql

# Uninstall phpMyAdmin & php-fpm
log_step "Purging phpMyAdmin and PHP-FPM dependencies..."
apt-get purge -y phpmyadmin php-fpm php-mysql php-mbstring php-xml 2>/dev/null || true
apt-get autoremove --purge -y 2>/dev/null || true
rm -rf /etc/phpmyadmin
rm -rf /usr/share/phpmyadmin
rm -rf /var/lib/phpmyadmin

# Final cleanup
log_step "Cleaning package manager cache..."
apt-get autoclean -y 2>/dev/null || true
apt-get clean -y 2>/dev/null || true

echo ""
log_success "${BOLD}MegoPanel and all associated services (Nginx, MariaDB, phpMyAdmin) have been fully removed!${RESET}"
echo "========================================="
