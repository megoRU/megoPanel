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
cd megoPanel
sudo scripts/uninstall-debian.sh
```

</details>

---

## License

This project is licensed under the **GNU General Public License v3.0**.

See the [LICENSE](https://github.com/megoRU/megoPanel/blob/main/LICENSE) file for details.
