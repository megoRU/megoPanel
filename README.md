# MegoPanel

Native Debian/Ubuntu server management panel written in Go with a React frontend.

## Features

- ⚡ Native installation (no Docker)
- 🐹 Go + Gin backend
- ⚛️ React + TypeScript frontend
- 🔐 JWT authentication + CSRF protection
- 🌐 English & Russian
- 🚀 One-command installation
- 🖥️ systemd service

## Requirements

- Debian 12+/Ubuntu 22.04+
- Root access

## Install

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl build-essential sqlite3 nodejs npm golang rsync

git clone https://github.com/megoRU/megoPanel
cd megoPanel
sudo scripts/install-debian.sh
```

Open:

```
http://SERVER_IP:8888
```

---

<details>
<summary><b>Development</b></summary>

### Backend

```bash
cd backend
go run ./cmd/server
```

### Frontend

```bash
cd frontend
npm install
npm run dev
```

Vite runs on **8889** and proxies `/api` to **8888**.

</details>

---

<details>
<summary><b>Build</b></summary>

Backend

```bash
cd backend
go build -trimpath -ldflags="-s -w" -o megopanel ./cmd/server
```

Frontend

```bash
cd frontend
npm install
npm run build
```

</details>

---

<details>
<summary><b>Configuration</b></summary>

Production config:

```yaml
environment: production

security:
  jwt_secret: "..."
  csrf_secret: "..."
  cookie_secure: true
```

</details>

---

<details>
<summary><b>systemd</b></summary>

```bash
sudo systemctl status megopanel
sudo systemctl restart megopanel
sudo journalctl -u megopanel -f
```

</details>

---

<details>
<summary><b>API</b></summary>

All endpoints are under:

```
/api/v1
```

Examples:

- `/setup/status`
- `/setup/admin`
- `/auth/login`
- `/auth/logout`
- `/auth/me`
- `/dashboard`
- `/install/mariadb`
- `/install/nginx`
- `/ws`

</details>

---

<details>
<summary><b>Complete removal</b></summary>

```bash
sudo systemctl stop megopanel 2>/dev/null || true
sudo systemctl disable megopanel 2>/dev/null || true

sudo rm -f /etc/systemd/system/megopanel.service
sudo systemctl daemon-reload
sudo systemctl reset-failed

sudo rm -f /usr/local/bin/megopanel

sudo rm -rf /etc/megopanel
sudo rm -rf /var/lib/megopanel
sudo rm -rf /opt/megopanel
sudo rm -rf /root/megopanel
sudo rm -rf ~/megopanel

sudo userdel -r megopanel 2>/dev/null || true
sudo groupdel megopanel 2>/dev/null || true

# Удаление Nginx
sudo systemctl stop nginx 2>/dev/null || true
sudo apt purge -y nginx nginx-common
sudo apt autoremove --purge -y
sudo rm -rf /etc/nginx
sudo rm -rf /var/log/nginx
sudo rm -rf /var/www

# Удаление MariaDB
sudo systemctl stop mariadb 2>/dev/null || true
sudo apt purge -y mariadb-server mariadb-client mariadb-common
sudo apt autoremove --purge -y
sudo rm -rf /etc/mysql
sudo rm -rf /var/lib/mysql
sudo rm -rf /var/log/mysql

# Удаление phpMyAdmin
sudo apt purge -y phpmyadmin
sudo apt autoremove --purge -y
sudo rm -rf /etc/phpmyadmin
sudo rm -rf /usr/share/phpmyadmin
sudo rm -rf /var/lib/phpmyadmin

# Очистка
sudo apt autoclean
sudo apt clean

# Проверка
dpkg -l | grep -Ei 'mariadb|mysql|phpmyadmin|nginx'
systemctl status mariadb
systemctl status nginx
which mysql
which nginx
```

</details>

---

## License

This project is licensed under the **GNU General Public License v3.0**.

See the [LICENSE](https://github.com/megoRU/megoPanel/blob/main/LICENSE) file for details.
