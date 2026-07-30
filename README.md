# MegoPanel

MegoPanel is a native Debian/Ubuntu server management panel inspired by modern products such as Vercel, Cloudflare, and GitHub. The backend is written in Go and the frontend is a separate React + TypeScript application.

## What is included

- Go 1.24+ backend with Gin, GORM, SQLite, JWT authentication, HttpOnly cookies, CSRF checks, CORS, security headers, REST API versioning, WebSocket support, YAML configuration, and structured logging.
- React + TypeScript + Vite frontend with TailwindCSS styling, route protection, TanStack Query, Axios, React Router, and i18next English/Russian translations.
- First-launch onboarding wizard for creating the only administrator account and installing MariaDB and Nginx.
- Native Debian/Ubuntu installation through `scripts/install-debian.sh` and a systemd unit.
- GitHub Actions CI for backend tests/build and frontend build.

## Requirements

Use a clean Debian or Ubuntu server with root access.

Minimum recommended system packages:

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl build-essential sqlite3 nodejs npm golang rsync
```

The installer also installs these packages automatically.

## Native Debian/Ubuntu installation

Clone the repository on the target server and run the installer:

```bash
git clone <repository-url> megopanel
cd megopanel
sudo scripts/install-debian.sh
```

The installer performs these actions:

1. Validates that the OS is Debian, Ubuntu, or Debian-like.
2. Installs native build/runtime dependencies with APT.
3. Builds the frontend with Vite.
4. Copies the built frontend into the backend static directory.
5. Builds the Go backend binary as `/usr/local/bin/megopanel`.
6. Creates `/etc/megopanel/config.yaml` if it does not exist.
7. Enables and starts the `megopanel.service` systemd unit.

Open the panel after installation:

```text
http://SERVER_IP:8080
```

## Manual development run

### Backend

```bash
cd backend
go mod download
go run ./cmd/server
```

By default the backend reads `backend/configs/config.yaml`. For production/native systemd runs, set `MEGOPANEL_CONFIG`:

```bash
MEGOPANEL_CONFIG=/etc/megopanel/config.yaml /usr/local/bin/megopanel
```

### Frontend

```bash
cd frontend
npm install
npm run dev
```

The Vite dev server proxies `/api` to `http://localhost:8080`.

## Build commands

```bash
cd backend
go test ./...
go build -trimpath -ldflags='-s -w' -o megopanel ./cmd/server
```

```bash
cd frontend
npm install
npm run build
```

## Runtime configuration

Configuration is YAML-based. The default file is `backend/configs/config.yaml`; native installs use `/etc/megopanel/config.yaml`.

Important production settings to change before exposing the panel:

```yaml
environment: production
security:
  jwt_secret: "replace-with-a-long-random-secret"
  csrf_secret: "replace-with-a-long-random-secret"
  cookie_secure: true
```

Use `cookie_secure: true` when serving the panel through HTTPS.

## systemd operations

```bash
sudo systemctl status megopanel
sudo systemctl restart megopanel
sudo journalctl -u megopanel -f
```

## API overview

All API routes are versioned under `/api/v1`:

- `GET /api/v1/setup/status`
- `POST /api/v1/setup/admin`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/me`
- `GET /api/v1/dashboard`
- `GET /api/v1/install/:name/status`
- `POST /api/v1/install/mariadb`
- `POST /api/v1/install/nginx`
- `GET /api/v1/ws`

## GitHub Actions

CI is defined in `.github/workflows/ci.yml`. It runs on pushes and pull requests and performs:

- Go dependency download, formatting check, tests, and backend build.
- Node dependency installation and frontend production build.

## Notes

This repository intentionally does not use Docker for deployment. The panel is designed to manage the host system directly, so installation is native on Debian/Ubuntu through systemd and APT.
