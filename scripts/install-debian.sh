#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="${PROJECT_ROOT}/backend"
FRONTEND_DIR="${PROJECT_ROOT}/frontend"
INSTALL_DIR="/opt/megopanel"
CONFIG_DIR="/etc/megopanel"
DATA_DIR="/var/lib/megopanel"

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
apt-get install -y ca-certificates curl build-essential sqlite3 nodejs npm golang

mkdir -p "${INSTALL_DIR}" "${CONFIG_DIR}" "${DATA_DIR}"
rsync -a --delete "${BACKEND_DIR}/" "${INSTALL_DIR}/backend/"
rsync -a --delete "${FRONTEND_DIR}/" "${INSTALL_DIR}/frontend/"

cd "${INSTALL_DIR}/frontend"
npm ci || npm install
npm run build
mkdir -p "${INSTALL_DIR}/backend/templates/frontend"
rsync -a --delete "${INSTALL_DIR}/frontend/dist/" "${INSTALL_DIR}/backend/templates/frontend/"

cd "${INSTALL_DIR}/backend"
go mod download
go build -trimpath -ldflags='-s -w' -o /usr/local/bin/megopanel ./cmd/server

if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
  cp "${INSTALL_DIR}/backend/configs/config.yaml" "${CONFIG_DIR}/config.yaml"
  sed -i 's#path: "panel.db"#path: "/var/lib/megopanel/panel.db"#' "${CONFIG_DIR}/config.yaml"
  sed -i 's#address: ":8080"#address: ":8080"#' "${CONFIG_DIR}/config.yaml"
fi

cp "${PROJECT_ROOT}/systemd/megopanel.service" /etc/systemd/system/megopanel.service
systemctl daemon-reload
systemctl enable --now megopanel

echo "MegoPanel is installed. Open http://SERVER_IP:8080"
