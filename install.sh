#!/usr/bin/env bash
set -euo pipefail

if [[ -d "/usr/local/go/bin" ]]; then
  export PATH="/usr/local/go/bin:${PATH}"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ "${SCRIPT_DIR}" == */scripts ]]; then
  PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
else
  PROJECT_ROOT="${SCRIPT_DIR}"
fi

BACKEND_DIR="${PROJECT_ROOT}/backend"
FRONTEND_DIR="${PROJECT_ROOT}/frontend"
INSTALL_DIR="/opt/megopanel"
CONFIG_DIR="/etc/megopanel"
DATA_DIR="/var/lib/megopanel"
APP_PORT="8888"
LOG_FILE="/tmp/megopanel-install.log"
CURL_PROXY_ARGS=()

# Define styling helpers
BLUE="\033[1;34m"
GREEN="\033[1;32m"
RED="\033[1;31m"
PURPLE="\033[1;35m"
BOLD="\033[1m"
RESET="\033[0m"

log_step() {
  echo -e "${BLUE}[•]${RESET} $1"
}

log_success() {
  echo -e "${GREEN}[✓]${RESET} $1"
}

log_error() {
  echo -e "${RED}[✗]${RESET} $1"
}

# Command runner that redirects output to a log file to keep the terminal tidy
run_cmd() {
  local cmd_name="$1"
  shift
  if ! "$@" >> "${LOG_FILE}" 2>&1; then
    log_error "Failed during: ${cmd_name}"
    echo -e "${RED}${BOLD}Error details (from ${LOG_FILE}):${RESET}"
    tail -n 30 "${LOG_FILE}"
    exit 1
  fi
}

if [[ -L "${LOG_FILE}" || ( -e "${LOG_FILE}" && ! -f "${LOG_FILE}" ) ]]; then
  log_error "Installer log path is not a regular file; refusing to write through it"
  exit 1
fi
touch "${LOG_FILE}"
chmod 0600 "${LOG_FILE}"

generate_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
  fi
}

install_go_toolchain() {
  local required="1.27.2"
  local installed=""
  if command -v go >/dev/null 2>&1; then
    installed="$(go version | awk '{print $3}' | sed 's/^go//')"
  fi
  if [[ -n "${installed}" && "$(printf '%s\n%s\n' "${required}" "${installed}" | sort -V | tail -n 1)" == "${installed}" ]]; then
    return 0
  fi

  local architecture archive_name checksum
  case "$(dpkg --print-architecture)" in
    amd64) architecture="amd64"; checksum="ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5" ;;
    arm64) architecture="arm64"; checksum="94f3e30b8e374bc285e7dadc11e0865726b9bc6e85b841ccceaabc0214c6b7c8" ;;
    armhf) architecture="armv6l"; checksum="e25a174051d8675f87ac720aedcd25bf89ac652c4b82f26aa47919524725ed95" ;;
    *) log_error "Go ${required} is not packaged for this CPU architecture"; return 1 ;;
  esac

  archive_name="go${required}.linux-${architecture}.tar.gz"
  local archive_path="/tmp/megopanel-${archive_name}"
  local install_path="/usr/local/lib/go-${required}"
  if [[ -x "${install_path}/bin/go" ]]; then
    installed="$("${install_path}/bin/go" version | awk '{print $3}' | sed 's/^go//')"
    if [[ "$(printf '%s\n%s\n' "${required}" "${installed}" | sort -V | tail -n 1)" == "${installed}" ]]; then
      export PATH="${install_path}/bin:${PATH}"
      return 0
    fi
    log_error "${install_path} exists but does not contain the required Go toolchain"
    return 1
  fi

  log_step "Downloading the supported Go ${required} toolchain for Linux..."
  run_cmd "download Go toolchain" curl "${CURL_PROXY_ARGS[@]}" --fail --show-error --location --connect-timeout 15 --retry 3 --retry-delay 2 --output "${archive_path}" "https://go.dev/dl/${archive_name}"
  if ! printf '%s  %s\n' "${checksum}" "${archive_path}" | sha256sum --check --status; then
    rm -f "${archive_path}"
    log_error "Go toolchain checksum validation failed"
    return 1
  fi
  local staging_path
  staging_path="$(mktemp -d /usr/local/lib/.megopanel-go.XXXXXX)"
  run_cmd "extract Go toolchain" tar -xzf "${archive_path}" --strip-components=1 -C "${staging_path}"
  if ! mv "${staging_path}" "${install_path}"; then
    rm -rf "${staging_path}"
    log_error "Could not install Go toolchain in ${install_path}"
    return 1
  fi
  rm -f "${archive_path}"
  export PATH="${install_path}/bin:${PATH}"
  log_success "Using Go $(go version | awk '{print $3}')"
}

install_node_toolchain() {
  local required="22.23.3"
  local installed=""
  if command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
    installed="$(node --version | sed 's/^v//')"
  fi
  if [[ -n "${installed}" && "$(printf '%s\n%s\n' "${required}" "${installed}" | sort -V | tail -n 1)" == "${installed}" ]]; then
    return 0
  fi

  local architecture archive_name checksum
  case "$(dpkg --print-architecture)" in
    amd64) architecture="x64"; checksum="1084aa36196bba4c3a5e69a1ee388a6e4ff729dad09445fbcd434b28fe3c24af" ;;
    arm64) architecture="arm64"; checksum="5ced2d48d1d7198739b7f86804de0171aefb6823b684b12341d3321afc3cb0b2" ;;
    armhf) architecture="armv7l"; checksum="8b99058ec6f4e372b9887f8ecb577a292adb132d59e5104356428a758fdb126c" ;;
    *) log_error "Node.js ${required} is not packaged for this CPU architecture"; return 1 ;;
  esac

  archive_name="node-v${required}-linux-${architecture}.tar.gz"
  local archive_path="/tmp/megopanel-${archive_name}"
  local install_path="/usr/local/lib/node-v${required}"
  if [[ -x "${install_path}/bin/node" && -x "${install_path}/bin/npm" ]]; then
    installed="$("${install_path}/bin/node" --version | sed 's/^v//')"
    if [[ "$(printf '%s\n%s\n' "${required}" "${installed}" | sort -V | tail -n 1)" == "${installed}" ]]; then
      export PATH="${install_path}/bin:${PATH}"
      return 0
    fi
    log_error "${install_path} exists but does not contain the required Node.js toolchain"
    return 1
  fi

  log_step "Downloading the supported Node.js ${required} toolchain for Linux..."
  run_cmd "download Node.js toolchain" curl "${CURL_PROXY_ARGS[@]}" --fail --show-error --location --connect-timeout 15 --retry 3 --retry-delay 2 --output "${archive_path}" "https://nodejs.org/dist/v${required}/${archive_name}"
  if ! printf '%s  %s\n' "${checksum}" "${archive_path}" | sha256sum --check --status; then
    rm -f "${archive_path}"
    log_error "Node.js toolchain checksum validation failed"
    return 1
  fi
  local staging_path
  staging_path="$(mktemp -d /usr/local/lib/.megopanel-node.XXXXXX)"
  run_cmd "extract Node.js toolchain" tar -xzf "${archive_path}" --strip-components=1 -C "${staging_path}"
  if ! mv "${staging_path}" "${install_path}"; then
    rm -rf "${staging_path}"
    log_error "Could not install Node.js toolchain in ${install_path}"
    return 1
  fi
  rm -f "${archive_path}"
  export PATH="${install_path}/bin:${PATH}"
  log_success "Using Node.js $(node --version)"
}

detect_server_ip() {
  local ip_services=(
    "https://api.ipify.org"
    "https://ifconfig.me/ip"
    "https://icanhazip.com"
  )
  local server_ip=""
  local ip_service=""

  for ip_service in "${ip_services[@]}"; do
    server_ip="$(curl --fail --silent --show-error --max-time 5 "${ip_service}" 2>/dev/null | tr -d '[:space:]' || true)"
    if [[ "${server_ip}" =~ ^[0-9A-Fa-f:.]+$ ]]; then
      echo "${server_ip}"
      return 0
    fi
  done

  hostname -I | awk '{print $1}'
}

# 1. Pre-installation checks
if [[ "${EUID}" -ne 0 ]]; then
  echo "Run this installer as root: sudo ./install.sh" >&2
  exit 1
fi

if [[ -f /etc/os-release ]]; then
  . /etc/os-release
fi

DISTRIBUTION_ID="${ID:-unknown}"
DISTRIBUTION_LIKE="${ID_LIKE:-}"
if [[ "${DISTRIBUTION_ID}" != "debian" && "${DISTRIBUTION_ID}" != "ubuntu" && "${DISTRIBUTION_LIKE}" != *"debian"* ]]; then
  echo "Unsupported distribution: ${DISTRIBUTION_ID}. Debian and Ubuntu are supported." >&2
  exit 1
fi

# Component Installers
install_nginx() {
  log_step "Checking Nginx..."
  if ! command -v nginx >/dev/null 2>&1 && [[ ! -f /usr/sbin/nginx ]]; then
    log_step "Installing Nginx..."
    run_cmd "install Nginx" apt-get install -y nginx
  fi
  run_cmd "enable Nginx" systemctl enable --now nginx
  log_success "Nginx is installed and running"
}

install_mariadb() {
  log_step "Checking MariaDB..."
  if ! command -v mysql >/dev/null 2>&1 && ! command -v mariadb >/dev/null 2>&1 && [[ ! -f /usr/sbin/mariadbd ]] && [[ ! -f /usr/sbin/mysqld ]]; then
    log_step "Installing MariaDB..."
    run_cmd "install MariaDB" apt-get install -y mariadb-server
  fi
  run_cmd "enable MariaDB" systemctl enable --now mariadb
  log_success "MariaDB is installed and running"
}

install_php() {
  log_step "Ensuring PHP-FPM and required modules are installed..."
  run_cmd "install PHP-FPM and modules" apt-get install -y php-fpm php-mysql php-mbstring php-xml php-curl php-zip php-gd
  local php_service
  php_service="$(systemctl list-unit-files --type=service --no-legend | awk '$1 ~ /^php[0-9.]+-fpm\.service$/ { sub(/\.service$/, "", $1); print $1; exit }')"
  if [[ -z "${php_service}" ]]; then
    log_error "PHP-FPM service was not found after installing its packages"
    return 1
  fi
  run_cmd "enable PHP-FPM" systemctl enable --now "${php_service}"
}

install_phpmyadmin() {
  log_step "Checking phpMyAdmin..."
  log_step "Ensuring phpMyAdmin PHP modules are installed..."
  install_php
  run_cmd "install phpMyAdmin archive tools" apt-get install -y tar curl
  if [[ -L "/var/www/phpmyadmin" ]]; then
    log_error "/var/www/phpmyadmin is a symlink; refusing to write through it"
    return 1
  fi
  if [[ ! -f "/var/www/phpmyadmin/index.php" ]]; then
    log_step "Downloading phpMyAdmin..."
    mkdir -p /var/www/phpmyadmin
    local archive_path="/tmp/phpmyadmin-5.2.1-all-languages.tar.gz"
    run_cmd "download phpMyAdmin" curl --fail --show-error --location --connect-timeout 15 --retry 3 --retry-delay 2 --output "${archive_path}" "https://files.phpmyadmin.net/phpMyAdmin/5.2.1/phpMyAdmin-5.2.1-all-languages.tar.gz"
    run_cmd "extract phpMyAdmin" tar -xzf "${archive_path}" --strip-components=1 -C /var/www/phpmyadmin
    rm -f "${archive_path}"
  else
    log_success "phpMyAdmin files already exist; preserving the existing installation"
  fi

  log_step "Configuring phpMyAdmin sign-on..."
  if [[ -L /var/www/phpmyadmin/config.inc.php ]]; then
    log_error "phpMyAdmin config is a symlink; refusing to write through it"
    return 1
  fi
  if [[ ! -f /var/www/phpmyadmin/config.inc.php ]]; then
    local blowfish_secret
    blowfish_secret="$(openssl rand -hex 16)"
    cat <<EOF > /var/www/phpmyadmin/config.inc.php
<?php
\$cfg['blowfish_secret'] = '${blowfish_secret}';
\$i = 0;
\$i++;

\$cfg['Servers'][\$i]['auth_type'] = 'signon';
\$cfg['Servers'][\$i]['SignonSession'] = 'SignonSession';
\$cfg['Servers'][\$i]['SignonURL'] = 'signon.php';
\$cfg['Servers'][\$i]['LogoutURL'] = 'signon.php?action=logout';
\$cfg['Servers'][\$i]['host'] = 'localhost';
\$cfg['Servers'][\$i]['compress'] = false;
\$cfg['Servers'][\$i]['AllowNoPassword'] = false;
EOF
  fi

  install -m 0644 "${BACKEND_DIR}/internal/service/templates/autologin.php" /var/www/phpmyadmin/autologin.php
  install -m 0644 "${BACKEND_DIR}/internal/service/templates/signon.php" /var/www/phpmyadmin/signon.php
  sed -i "s/__MEGOPANEL_API_PORT__/${APP_PORT}/g" /var/www/phpmyadmin/signon.php
  log_step "Setting phpMyAdmin files ownership..."
  chown -R root:www-data /var/www/phpmyadmin
  chmod 0755 /var/www/phpmyadmin
  chmod 0640 /var/www/phpmyadmin/config.inc.php

  # Setup Nginx configuration for phpMyAdmin on port 8080
  log_step "Configuring PHP-FPM socket..."
  local php_service
  php_service="$(systemctl list-unit-files --type=service --no-legend | awk '$1 ~ /^php[0-9.]+-fpm\.service$/ { sub(/\.service$/, "", $1); print $1; exit }')"
  if [[ -z "${php_service}" ]]; then
    log_error "PHP-FPM service was not found"
    return 1
  fi
  run_cmd "enable PHP-FPM" systemctl enable --now "${php_service}"

  # Find PHP-FPM socket
  local socket_path=""
  for p in "/run/php" "/var/run/php"; do
    if [[ -d "${p}" ]]; then
      local found_sock
      found_sock=$(find "${p}" -name "php*-fpm.sock" | head -n 1)
      if [[ -n "${found_sock}" ]]; then
        socket_path="${found_sock}"
        break
      fi
    fi
  done
  if [[ -z "${socket_path}" ]]; then
    log_error "PHP-FPM socket was not found"
    return 1
  fi

  log_step "Configuring Nginx reverse proxy on port 8080..."
  mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
  if [[ -L /etc/nginx/sites-available/phpmyadmin ]]; then
    log_error "Nginx phpMyAdmin site config is a symlink; refusing to write through it"
    return 1
  fi
  cat <<EOF2 > /etc/nginx/sites-available/phpmyadmin
server {
    listen 8080;
    listen [::]:8080;
    root /var/www/phpmyadmin;
    index index.php index.html index.htm;
    server_name _;
    location / {
        try_files \$uri \$uri/ =404;
    }
    location ~ \.php\$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:${socket_path};
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
    }
}
EOF2

  rm -f /etc/nginx/sites-enabled/phpmyadmin
  ln -s /etc/nginx/sites-available/phpmyadmin /etc/nginx/sites-enabled/phpmyadmin

  run_cmd "validate Nginx configuration" nginx -t
  run_cmd "restart Nginx" systemctl restart nginx

  log_step "Verifying phpMyAdmin Auto Login with health check..."
  if curl --fail --silent --show-error --max-time 10 --output /dev/null "http://127.0.0.1:8080/"; then
    log_success "Installed successfully"
  else
    log_error "phpMyAdmin Auto Login health check failed!"
    exit 1
  fi
}

# Print ASCII Banner
echo -e "${PURPLE}${BOLD}"
echo " __  __                    ____                  _"
echo "|  \/  | ___  __ _  ___   |  _ \ __ _ _ __   ___| |"
echo "| |\/| |/ _ \/ _\` |/ _ \  | |_) / _\` | '_ \ / _ \ |"
echo "| |  | |  __/ (_| | (_) | |  __/ (_| | | | |  __/ |"
echo "|_|  |_|\___|\__, |\___/  |_|   \__,_|_| |_|\___|_|"
echo "              |___/                                "
echo -e "${RESET}"

echo -e "${BOLD}MegoPanel Installation Wizard${RESET}"
echo -e "Logs will be recorded to ${BOLD}${LOG_FILE}${RESET}"
echo "========================================="
echo "" > "${LOG_FILE}"

# 2. Interactive Component Selection
echo "=================================="
echo "Выберите компоненты"
echo ""
echo "[1] Nginx"
echo ""
echo "[2] MariaDB"
echo ""
echo "[3] phpMyAdmin"
echo ""
echo "[4] Всё"
echo ""
echo -n "Введите номера через пробел: "
read -r user_choices
echo "=================================="

nginx_selected=false
mariadb_selected=false
phpmyadmin_selected=false

for choice in $user_choices; do
  case "$choice" in
    1) nginx_selected=true ;;
    2) mariadb_selected=true ;;
    3) phpmyadmin_selected=true ;;
    4)
      nginx_selected=true
      mariadb_selected=true
      phpmyadmin_selected=true
      ;;
  esac
done

# 3. Package updates & Base Dependencies for the panel
log_step "Updating package manager repositories..."
run_cmd "apt-get update" apt-get update

log_step "Installing MegoPanel system dependencies..."
run_cmd "apt-get install" apt-get install -y ca-certificates curl build-essential sqlite3 rsync openssl
install_go_toolchain
install_node_toolchain

# 4. Optional Component Installations
if [[ "${nginx_selected}" == "true" ]]; then
  install_nginx
fi

if [[ "${mariadb_selected}" == "true" ]]; then
  install_mariadb
fi

if [[ "${phpmyadmin_selected}" == "true" ]]; then
  install_phpmyadmin
fi

# 5. Directory preparation
log_step "Preparing installation directories..."
run_cmd "mkdir -p" mkdir -p "${INSTALL_DIR}" "${CONFIG_DIR}" "${DATA_DIR}" /var/backups/megopanel
run_cmd "secure backup directory" chmod 0700 /var/backups/megopanel
run_cmd "rsync backend" rsync -a --delete "${BACKEND_DIR}/" "${INSTALL_DIR}/backend/"
run_cmd "rsync frontend" rsync -a --delete "${FRONTEND_DIR}/" "${INSTALL_DIR}/frontend/"

# 6. Build/Prepare Frontend
log_step "Preparing frontend assets..."
if [[ -d "${FRONTEND_DIR}/dist" ]]; then
  log_step "Using pre-built frontend from ${FRONTEND_DIR}/dist"
  mkdir -p "${INSTALL_DIR}/frontend/dist"
  run_cmd "copy pre-built dist" cp -r "${FRONTEND_DIR}/dist/"* "${INSTALL_DIR}/frontend/dist/"
else
  log_step "Installing frontend project packages (npm ci)..."
  cd "${INSTALL_DIR}/frontend"
  run_cmd "npm ci" npm ci

  log_step "Building frontend production assets (npm run build)..."
  run_cmd "npm build" npm run build
fi

log_step "Synchronizing assets to backend directory..."
run_cmd "mkdir frontend templates" mkdir -p "${INSTALL_DIR}/backend/templates/frontend"
run_cmd "rsync dist" rsync -a --delete "${INSTALL_DIR}/frontend/dist/" "${INSTALL_DIR}/backend/templates/frontend/"

# 7. Build Backend
log_step "Preparing backend modules & dependencies (go mod tidy)..."
cd "${INSTALL_DIR}/backend"
run_cmd "go mod tidy" go mod tidy

log_step "Compiling backend service binary..."
run_cmd "go build" go build -trimpath -ldflags='-s -w' -o /usr/local/bin/megopanel ./cmd/server

# 8. Service Configuration
log_step "Configuring application service & security parameters..."
if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
  JWT_SECRET="$(generate_secret)"
  CSRF_SECRET="$(generate_secret)"
  run_cmd "cp config" cp "${INSTALL_DIR}/backend/configs/config.yaml" "${CONFIG_DIR}/config.yaml"
  run_cmd "sed DB path" sed -i 's#path: "panel.db"#path: "/var/lib/megopanel/panel.db"#' "${CONFIG_DIR}/config.yaml"
  run_cmd "sed port" sed -i "s#address: \":8888\"#address: \":${APP_PORT}\"#" "${CONFIG_DIR}/config.yaml"
  run_cmd "sed secret" sed -i "s#jwt_secret: \"change-me-in-production\"#jwt_secret: \"${JWT_SECRET}\"#" "${CONFIG_DIR}/config.yaml"
  run_cmd "sed CSRF secret" sed -i "s#csrf_secret: \"change-me-csrf\"#csrf_secret: \"${CSRF_SECRET}\"#" "${CONFIG_DIR}/config.yaml"
fi

if ! grep -Eq '^[[:space:]]*jwt_secret:[[:space:]]*"[^\"]{32,}"' "${CONFIG_DIR}/config.yaml" || grep -Eqi '^[[:space:]]*jwt_secret:[[:space:]]*"change-me' "${CONFIG_DIR}/config.yaml"; then
  JWT_SECRET="$(generate_secret)"
  run_cmd "replace weak JWT secret" sed -i -E "s#^([[:space:]]*jwt_secret:).*#\1 \"${JWT_SECRET}\"#" "${CONFIG_DIR}/config.yaml"
fi
if ! grep -Eq '^[[:space:]]*csrf_secret:[[:space:]]*"[^\"]{32,}"' "${CONFIG_DIR}/config.yaml" || grep -Eqi '^[[:space:]]*csrf_secret:[[:space:]]*"change-me' "${CONFIG_DIR}/config.yaml"; then
  CSRF_SECRET="$(generate_secret)"
  run_cmd "replace weak CSRF secret" sed -i -E "s#^([[:space:]]*csrf_secret:).*#\1 \"${CSRF_SECRET}\"#" "${CONFIG_DIR}/config.yaml"
fi
run_cmd "secure panel config" chmod 0600 "${CONFIG_DIR}/config.yaml"

if [[ -L "${CONFIG_DIR}/setup.token" ]]; then
  log_error "Setup token path is a symlink; refusing to write through it"
  exit 1
fi
admin_count=0
if [[ -f "${DATA_DIR}/panel.db" ]]; then
  admin_count="$(sqlite3 "${DATA_DIR}/panel.db" 'SELECT COUNT(*) FROM admins;' 2>/dev/null || printf '0')"
fi
if ! [[ "${admin_count}" =~ ^[0-9]+$ ]]; then admin_count=0; fi
SETUP_TOKEN=""
if (( admin_count == 0 )); then
  if [[ ! -f "${CONFIG_DIR}/setup.token" ]]; then
    SETUP_TOKEN="$(generate_secret)"
    printf '%s\n' "${SETUP_TOKEN}" > "${CONFIG_DIR}/setup.token"
  else
    IFS= read -r SETUP_TOKEN < "${CONFIG_DIR}/setup.token" || true
    if ! [[ "${SETUP_TOKEN}" =~ ^[0-9a-f]{64}$ ]]; then
      SETUP_TOKEN="$(generate_secret)"
      printf '%s\n' "${SETUP_TOKEN}" > "${CONFIG_DIR}/setup.token"
    fi
  fi
  run_cmd "secure setup token" chmod 0600 "${CONFIG_DIR}/setup.token"
fi
run_cmd "secure panel data directory" chmod 0700 "${DATA_DIR}" "${CONFIG_DIR}"

# Copy systemd service
run_cmd "cp systemd service" cp "${PROJECT_ROOT}/systemd/megopanel.service" /etc/systemd/system/megopanel.service

log_step "Starting MegoPanel systemd service..."
run_cmd "systemctl daemon-reload" systemctl daemon-reload
run_cmd "systemctl enable --now" systemctl enable --now megopanel

# Complete!
SERVER_IP="$(detect_server_ip)"
echo ""
log_success "${BOLD}MegoPanel successfully installed!${RESET}"
echo -e "Access link: ${BLUE}${BOLD}http://${SERVER_IP}:${APP_PORT}${RESET}"
if [[ -n "${SETUP_TOKEN}" ]]; then
  echo "One-time setup token (enter it in the first-run administrator form): ${SETUP_TOKEN}"
fi
echo "========================================="
