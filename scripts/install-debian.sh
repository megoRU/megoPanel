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

# Proxy settings
ALL_PROXY=""
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

generate_secret() {
  openssl rand -hex 32 2>/dev/null || echo "default_secret_key_1234567890abcdef"
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
    server_ip="$(
      curl "${CURL_PROXY_ARGS[@]}" \
        --fail \
        --silent \
        --show-error \
        --max-time 5 \
        "${ip_service}" 2>/dev/null | tr -d '[:space:]' || true
    )"

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

echo ""
read -rp "Использовать SOCKS5-прокси? [y/N]: " USE_PROXY

if [[ "$USE_PROXY" =~ ^[Yy]$ ]]; then
    read -rp "IP/домен прокси [127.0.0.1]: " PROXY_HOST
    read -rp "Порт прокси [1080]: " PROXY_PORT

    PROXY_HOST=${PROXY_HOST:-127.0.0.1}
    PROXY_PORT=${PROXY_PORT:-1080}

    ALL_PROXY="socks5://${PROXY_HOST}:${PROXY_PORT}"

    export ALL_PROXY
    export all_proxy="$ALL_PROXY"
    export HTTP_PROXY="$ALL_PROXY"
    export HTTPS_PROXY="$ALL_PROXY"
    export http_proxy="$ALL_PROXY"
    export https_proxy="$ALL_PROXY"

    CURL_PROXY_ARGS=(--proxy "$ALL_PROXY")

    log_success "Используется прокси ${ALL_PROXY}"
fi

# Component Installers
install_nginx() {
  log_step "Checking Nginx..."
  if command -v nginx >/dev/null 2>&1 || [[ -f /usr/sbin/nginx ]]; then
    log_success "Already installed"
    return 0
  fi

  log_step "Installing Nginx..."
  apt-get install -y nginx >> "${LOG_FILE}" 2>&1
  systemctl enable --now nginx >> "${LOG_FILE}" 2>&1
  systemctl restart nginx >> "${LOG_FILE}" 2>&1
  log_success "Installed successfully"
}

install_mariadb() {
  log_step "Checking MariaDB..."
  if command -v mysql >/dev/null 2>&1 || command -v mariadb >/dev/null 2>&1 || [[ -f /usr/sbin/mariadbd ]] || [[ -f /usr/sbin/mysqld ]]; then
    log_success "Already installed"
    return 0
  fi

  log_step "Installing MariaDB..."
  apt-get install -y mariadb-server >> "${LOG_FILE}" 2>&1
  systemctl enable --now mariadb >> "${LOG_FILE}" 2>&1
  systemctl restart mariadb >> "${LOG_FILE}" 2>&1
  log_success "Installed successfully"
}

install_php() {
  log_step "Checking PHP..."
  if command -v php >/dev/null 2>&1; then
    log_success "Already installed"
    return 0
  fi

  log_step "Installing PHP and modules..."
  apt-get install -y php-fpm php-mysql php-mbstring php-xml php-curl php-zip php-gd >> "${LOG_FILE}" 2>&1
  log_success "Installed successfully"
}

install_phpmyadmin() {
  log_step "Checking phpMyAdmin..."
  if [[ -d "/var/www/phpmyadmin" && -f "/var/www/phpmyadmin/config.inc.php" ]]; then
    log_success "Already installed"
    return 0
  fi

  log_step "Ensuring phpMyAdmin PHP modules are installed..."
  install_php
  apt-get install -y tar curl >> "${LOG_FILE}" 2>&1

  log_step "Downloading phpMyAdmin..."
  mkdir -p /var/www/phpmyadmin
  local archive_path="/tmp/phpmyadmin-5.2.1-all-languages.tar.gz"
  curl "${CURL_PROXY_ARGS[@]}" \
    --fail \
    --show-error \
    --location \
    --connect-timeout 15 \
    --retry 3 \
    --retry-delay 2 \
    --output "${archive_path}" \
    "https://files.phpmyadmin.net/phpMyAdmin/5.2.1/phpMyAdmin-5.2.1-all-languages.tar.gz" \
    >> "${LOG_FILE}" 2>&1
  tar -xzf "${archive_path}" --strip-components=1 -C /var/www/phpmyadmin >> "${LOG_FILE}" 2>&1
  rm -f "${archive_path}"

  log_step "Generating phpMyAdmin config and scripts..."
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

  cat <<'EOF' > /var/www/phpmyadmin/autologin.php
<?php
if (isset($_GET['token'])) {
    $token = preg_replace('/[^a-zA-Z0-9-]/', '', $_GET['token']);
    header('Location: signon.php?token=' . $token);
    exit;
}
header('Location: index.php');
exit;
EOF

  cat <<EOF > /var/www/phpmyadmin/signon.php
<?php
session_name('SignonSession');
session_start();

if (isset(\$_GET['action']) && \$_GET['action'] === 'logout') {
    unset(\$_SESSION['PMA_single_signon_user']);
    unset(\$_SESSION['PMA_single_signon_password']);
    unset(\$_SESSION['PMA_single_signon_host']);
    session_destroy();
    header('Location: http://' . \$_SERVER['HTTP_HOST'] . ':${APP_PORT}/');
    exit;
}

\$token = isset(\$_GET['token']) ? preg_replace('/[^a-zA-Z0-9-]/', '', \$_GET['token']) : '';

if (empty(\$token)) {
    show_error_page("Token is missing or invalid.", "Please try logging in to phpMyAdmin again from the MegoPanel dashboard.");
}

\$apiUrl = "http://127.0.0.1:${APP_PORT}/internal/phpmyadmin/token?token=" . urlencode(\$token);

\$ch = curl_init();
curl_setopt(\$ch, CURLOPT_URL, \$apiUrl);
curl_setopt(\$ch, CURLOPT_RETURNTRANSFER, true);
curl_setopt(\$ch, CURLOPT_TIMEOUT, 5);
\$response = curl_exec(\$ch);
\$httpCode = curl_getinfo(\$ch, CURLINFO_HTTP_CODE);
curl_close(\$ch);

if (\$httpCode !== 200 || !\$response) {
    \$ctx = stream_context_create([
        'http' => [
            'timeout' => 5,
            'ignore_errors' => true
        ]
    ]);
    \$response = @file_get_contents(\$apiUrl, false, \$ctx);
    \$httpCode = 0;
    if (isset(\$http_response_header[0])) {
        preg_match('{HTTP\/\S+\s+(\d+)}', \$http_response_header[0], \$matches);
        if (isset(\$matches[1])) {
            \$httpCode = intval(\$matches[1]);
        }
    }
}

if (\$httpCode !== 200 || !\$response) {
    show_error_page("Authentication failed.", "The autologin token is invalid, expired (expired after 15 seconds), or already used. Please go back to the MegoPanel dashboard and try again.");
}

\$data = json_decode(\$response, true);
if (!\$data || !isset(\$data['username']) || !isset(\$data['password'])) {
    show_error_page("Malformed API response.", "The authentication service returned an invalid response format.");
}

\$_SESSION['PMA_single_signon_user'] = \$data['username'];
\$_SESSION['PMA_single_signon_password'] = \$data['password'];
\$_SESSION['PMA_single_signon_host'] = 'localhost';

header('Location: index.php');
exit;

function show_error_page(\$title, \$message) {
    header("HTTP/1.1 403 Forbidden");
    ?>
    <!DOCTYPE html>
    <html lang="en">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>phpMyAdmin Autologin Error</title>
        <style>
            body {
                background: #0f0f11;
                color: #e4e4e7;
                font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
                display: flex;
                align-items: center;
                justify-content: center;
                min-height: 100vh;
                margin: 0;
            }
            .card {
                background: #18181b;
                border: 1px solid #27272a;
                border-radius: 8px;
                padding: 32px;
                max-width: 480px;
                width: 100%;
                box-shadow: 0 4px 20px rgba(0, 0, 0, 0.4);
                text-align: center;
            }
            .icon {
                color: #ef4444;
                font-size: 48px;
                margin-bottom: 16px;
            }
            h1 {
                font-size: 20px;
                font-weight: 600;
                margin: 0 0 12px 0;
                color: #ffffff;
            }
            p {
                font-size: 14px;
                color: #a1a1aa;
                line-height: 1.5;
                margin: 0 0 24px 0;
            }
            .btn {
                display: inline-block;
                background: #ffffff;
                color: #0f0f11;
                font-weight: 600;
                font-size: 13px;
                text-decoration: none;
                padding: 10px 20px;
                border-radius: 6px;
                transition: background 0.15s ease;
            }
            .btn:hover {
                background: #e4e4e7;
            }
        </style>
    </head>
    <body>
        <div class="card">
            <div class="icon">⚠️</div>
            <h1><?php echo htmlspecialchars(\$title); ?></h1>
            <p><?php echo htmlspecialchars(\$message); ?></p>
            <a href="http://<?php echo htmlspecialchars(\$_SERVER['HTTP_HOST']); ?>:${APP_PORT}/" class="btn">Back to Dashboard</a>
        </div>
    </body>
    </html>
    <?php
    exit;
}
EOF

  log_step "Setting phpMyAdmin files ownership..."
  chown -R www-data:www-data /var/www/phpmyadmin

  # Setup Nginx configuration for phpMyAdmin on port 8080
  log_step "Configuring PHP-FPM socket..."
  local php_service="php-fpm"
  if command -v systemctl >/dev/null 2>&1; then
    local detected_svc
    detected_svc=$(systemctl list-unit-files | grep -E '^php[0-9.]+-fpm\.service' || true)
    if [[ -n "${detected_svc}" ]]; then
      php_service=$(echo "${detected_svc}" | head -n 1 | awk '{print $1}' | sed 's/\.service//')
    fi
  fi

  systemctl reset-failed "${php_service}" || true
  systemctl enable --now "${php_service}" >> "${LOG_FILE}" 2>&1
  systemctl restart "${php_service}" >> "${LOG_FILE}" 2>&1

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
    socket_path="/run/php/${php_service}.sock"
  fi

  log_step "Configuring Nginx reverse proxy on port 8080..."
  mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
  cat <<EOF2 > /etc/nginx/sites-available/phpmyadmin
server {
    listen 8080 default_server;
    listen [::]:8080 default_server;
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

  if [[ -f /etc/nginx/sites-enabled/default ]]; then
    rm -f /etc/nginx/sites-enabled/default
  fi

  systemctl restart nginx >> "${LOG_FILE}" 2>&1

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

log_step "Installing MegoPanel dependencies (Go, Node.js, SQLite, rsync)..."
run_cmd "apt-get install" apt-get install -y ca-certificates curl build-essential sqlite3 nodejs npm golang rsync openssl

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
run_cmd "mkdir -p" mkdir -p "${INSTALL_DIR}" "${CONFIG_DIR}" "${DATA_DIR}" "/etc/mysql"
run_cmd "rsync backend" rsync -a --delete "${BACKEND_DIR}/" "${INSTALL_DIR}/backend/"
run_cmd "rsync frontend" rsync -a --delete "${FRONTEND_DIR}/" "${INSTALL_DIR}/frontend/"

# 6. Build/Prepare Frontend
log_step "Preparing frontend assets..."
if [[ -d "${FRONTEND_DIR}/dist" ]]; then
  log_step "Using pre-built frontend from ${FRONTEND_DIR}/dist"
  mkdir -p "${INSTALL_DIR}/frontend/dist"
  run_cmd "copy pre-built dist" cp -r "${FRONTEND_DIR}/dist/"* "${INSTALL_DIR}/frontend/dist/"
else
  log_step "Installing frontend project packages (npm install)..."
  cd "${INSTALL_DIR}/frontend"
  run_cmd "npm install" npm install

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
