#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="${PROJECT_ROOT}/backend"
FRONTEND_DIR="${PROJECT_ROOT}/frontend"
INSTALL_DIR="/opt/megopanel"
CONFIG_DIR="/etc/megopanel"
DATA_DIR="/var/lib/megopanel"
APP_PORT="8888"
LOG_FILE="/tmp/megopanel-install.log"

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

generate_secret() {
  openssl rand -hex 32
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
    server_ip="$(curl --fail --silent --show-error --max-time 5 "${ip_service}" 2>/dev/null | tr -d '[:space:]')"
    if [[ "${server_ip}" =~ ^[0-9A-Fa-f:.]+$ ]]; then
      echo "${server_ip}"
      return 0
    fi
  done

  hostname -I | awk '{print $1}'
}

# 1. Pre-installation checks
if [[ "${EUID}" -ne 0 ]]; then
  echo "Run this installer as root: sudo scripts/install-debian.sh" >&2
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

# 2. Package updates
log_step "Updating package manager repositories..."
run_cmd "apt-get update" apt-get update

log_step "Installing system dependencies (Go, Node.js, SQLite, rsync)..."
run_cmd "apt-get install" apt-get install -y ca-certificates curl build-essential sqlite3 nodejs npm golang rsync openssl

# 3. Directory preparation
log_step "Preparing installation directories..."
run_cmd "mkdir -p" mkdir -p "${INSTALL_DIR}" "${CONFIG_DIR}" "${DATA_DIR}" "/etc/mysql"
run_cmd "rsync backend" rsync -a --delete "${BACKEND_DIR}/" "${INSTALL_DIR}/backend/"
run_cmd "rsync frontend" rsync -a --delete "${FRONTEND_DIR}/" "${INSTALL_DIR}/frontend/"

# 4. Build Frontend
log_step "Installing frontend project packages (npm install)..."
cd "${INSTALL_DIR}/frontend"
run_cmd "npm install" npm install

log_step "Building frontend production assets (npm run build)..."
run_cmd "npm build" npm run build

log_step "Synchronizing assets to backend directory..."
run_cmd "mkdir frontend templates" mkdir -p "${INSTALL_DIR}/backend/templates/frontend"
run_cmd "rsync dist" rsync -a --delete "${INSTALL_DIR}/frontend/dist/" "${INSTALL_DIR}/backend/templates/frontend/"

# 5. Build Backend
log_step "Preparing backend modules & dependencies (go mod tidy)..."
cd "${INSTALL_DIR}/backend"
run_cmd "go mod tidy" go mod tidy

log_step "Compiling backend service binary..."
run_cmd "go build" go build -trimpath -ldflags='-s -w' -o /usr/local/bin/megopanel ./cmd/server

# 6. Service Configuration
log_step "Configuring application service & security parameters..."
if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
  JWT_SECRET="$(generate_secret)"
  run_cmd "cp config" cp "${INSTALL_DIR}/backend/configs/config.yaml" "${CONFIG_DIR}/config.yaml"
  run_cmd "sed DB path" sed -i 's#path: "panel.db"#path: "/var/lib/megopanel/panel.db"#' "${CONFIG_DIR}/config.yaml"
  run_cmd "sed port" sed -i "s#address: \":8888\"#address: \":${APP_PORT}\"#" "${CONFIG_DIR}/config.yaml"
  run_cmd "sed secret" sed -i "s#jwt_secret: \"change-me-in-production\"#jwt_secret: \"${JWT_SECRET}\"#" "${CONFIG_DIR}/config.yaml"
fi

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
echo "========================================="
