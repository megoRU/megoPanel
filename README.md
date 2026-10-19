# MegoPanel

MegoPanel is a native server management panel with a Go/Gin API and a React + TypeScript interface. It installs as a systemd service and manages its own SQLite state, MariaDB access, phpMyAdmin sign-on, updates, and backups.

## Supported systems

- Debian 12 or newer
- Ubuntu 22.04 or newer
- Root access and an internet connection during installation

The installer builds the application on the Linux server and installs its Linux dependencies. Nginx, MariaDB, and phpMyAdmin can be selected during setup.

## Install

```bash
git clone https://github.com/megoRU/megoPanel.git
cd megoPanel
sudo scripts/install-debian.sh
```

Open the address printed at the end of installation. The installer prints a one-time administrator setup token. Enter it on the first-run setup page along with the administrator name and password. The token is stored in `/etc/megopanel/setup.token` with root-only access and removed after successful setup.

The initial address uses HTTP. Keep it on a trusted private network until you configure HTTPS. For TLS termination, set `security.cookie_secure: true` in `/etc/megopanel/config.yaml`, configure a reverse proxy with the panel's configured `server.address` as its upstream, then restart the service:

```bash
sudo systemctl restart megopanel
```

Do not expose the panel or phpMyAdmin directly to the public internet without TLS and an appropriate access policy.

## MariaDB and phpMyAdmin

Install MariaDB from the installer or install it separately first. In the Databases view, configure panel access with the current MariaDB root password, or leave the password blank when local Unix socket authentication is available. MegoPanel does not change the MariaDB root password. It provisions a separate `megopanel` account and keeps its credential in the panel's protected SQLite database.

phpMyAdmin sign-on uses a short-lived, single-use token passed in the URL fragment, then exchanges it through a local server request. The database password is not put in the URL. If sign-on fails, check that MariaDB is running and that the phpMyAdmin configuration uses the MegoPanel sign-on settings; the panel re-applies its managed sign-on files when it starts.

The Websites view creates a static Nginx virtual host and a starter `index.html` under `/var/www/<domain>`. The domain must resolve to this server. Removing a site disables its MegoPanel-managed Nginx config and removes its panel record, while preserving the website files.

## Backups

The Backups view supports manual and daily scheduled archives. Select panel data and configuration, MariaDB application databases, and/or website files. MariaDB is saved as a consistent logical dump, not by archiving its live data directory. Archives have a SHA-256 sidecar, are created with root-only permissions, and can be verified, downloaded, deleted, or restored. Restore checks the archive and version, creates a pre-restore backup, and reports which components were restored.

Automatic backups use the server's local time. Configure the component set, hour, storage directory, maximum number of archives, and age limit in the Backups view. Storage is restricted to `/var/lib/megopanel/backups` and `/var/backups/megopanel` (including subdirectories).

Archives are **not encrypted** and stay on the server by default. A backup on the same disk does not protect against server or disk loss; download copies or move them to separately secured external storage. Website restore overwrites matching archived files and leaves files absent from the archive in place. phpMyAdmin application files are excluded.

## Updates and diagnostics

Updates are downloaded from GitHub releases only after the version and SHA-256 sidecar are checked. To inspect the service:

```bash
sudo systemctl status megopanel
sudo journalctl -u megopanel -f
```

The panel keeps recent backup operation results in the Backups view. Installer output and package diagnostics are written to `/tmp/megopanel-install.log`.

If an update leaves the service unable to start, the previous binary and templates are kept beside the live files. After confirming both rollback files exist, restore them and restart:

```bash
sudo systemctl stop megopanel
sudo cp /usr/local/bin/megopanel.previous /usr/local/bin/megopanel
sudo rm -rf /opt/megopanel/backend/templates
sudo mv /opt/megopanel/backend/templates.previous /opt/megopanel/backend/templates
sudo systemctl start megopanel
```

## Development

Backend:

```bash
cd backend
go test ./...
go vet ./...
go run ./cmd/server
```

Frontend:

```bash
cd frontend
npm ci
npm run build
npm run dev
```

The development frontend runs on port 8889 and proxies API requests to port 8888. Development configuration must use random JWT and CSRF secrets of at least 32 bytes; do not use the sample placeholders.

## Remove

```bash
sudo scripts/uninstall-debian.sh
```

The uninstaller asks before removing the application. It preserves `/etc/megopanel` and `/var/lib/megopanel` unless you type `DELETE` at the data-removal prompt. Backups in `/var/backups/megopanel` are always preserved. It leaves Nginx, MariaDB, phpMyAdmin, website files, and their data in place.

## License

GNU General Public License v3.0. See [LICENSE](LICENSE).
