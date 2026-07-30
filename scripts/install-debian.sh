#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="${PROJECT_ROOT}/backend"
FRONTEND_DIR="${PROJECT_ROOT}/frontend"
INSTALL_DIR="/opt/megopanel"
CONFIG_DIR="/etc/megopanel"
DATA_DIR="/var/lib/megopanel"
APP_PORT="8888"

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

apt-get update
apt-get install -y ca-certificates curl build-essential sqlite3 nodejs npm golang rsync openssl

mkdir -p "${INSTALL_DIR}" "${CONFIG_DIR}" "${DATA_DIR}" "/etc/mysql"
rsync -a --delete "${BACKEND_DIR}/" "${INSTALL_DIR}/backend/"
rsync -a --delete "${FRONTEND_DIR}/" "${INSTALL_DIR}/frontend/"

cd "${INSTALL_DIR}/frontend"
npm ci || npm install
npm run build
mkdir -p "${INSTALL_DIR}/backend/templates/frontend"
rsync -a --delete "${INSTALL_DIR}/frontend/dist/" "${INSTALL_DIR}/backend/templates/frontend/"

cd "${INSTALL_DIR}/backend"
go mod tidy
go build -trimpath -ldflags='-s -w' -o /usr/local/bin/megopanel ./cmd/server

if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
  JWT_SECRET="$(generate_secret)"
  cp "${INSTALL_DIR}/backend/configs/config.yaml" "${CONFIG_DIR}/config.yaml"
  sed -i 's#path: "panel.db"#path: "/var/lib/megopanel/panel.db"#' "${CONFIG_DIR}/config.yaml"
  sed -i "s#address: \":8888\"#address: \":${APP_PORT}\"#" "${CONFIG_DIR}/config.yaml"
  sed -i "s#jwt_secret: \"change-me-in-production\"#jwt_secret: \"${JWT_SECRET}\"#" "${CONFIG_DIR}/config.yaml"
fi

cp "${PROJECT_ROOT}/systemd/megopanel.service" /etc/systemd/system/megopanel.service
systemctl daemon-reload
systemctl enable --now megopanel

SERVER_IP="$(detect_server_ip)"
echo "MegoPanel is installed. Open http://${SERVER_IP}:${APP_PORT}"
