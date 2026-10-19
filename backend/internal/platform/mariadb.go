package platform

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// MariaDBQuery runs a fixed SQL operation without placing credentials or SQL
// text in the process argument list. An empty root password uses the local
// Unix socket, which is the default authentication method on Debian/Ubuntu.
func MariaDBQuery(ctx context.Context, username, password, query string) ([]byte, error) {
	if !validMariaDBUsername(username) {
		return nil, errors.New("invalid database username")
	}
	var defaultsPath string
	if password != "" {
		if strings.ContainsAny(password, "\x00\r\n") {
			return nil, errors.New("database password contains unsupported control characters")
		}
		file, err := os.CreateTemp("", "megopanel-mariadb-*.cnf")
		if err != nil {
			return nil, fmt.Errorf("create temporary database client config: %w", err)
		}
		defaultsPath = file.Name()
		defer os.Remove(defaultsPath)
		if err := file.Chmod(0600); err != nil {
			file.Close()
			return nil, fmt.Errorf("secure temporary database client config: %w", err)
		}
		quotedPassword := strings.NewReplacer("\\", "\\\\", `"`, `\"`).Replace(password)
		if _, err := fmt.Fprintf(file, "[client]\nuser=%q\npassword=\"%s\"\n", username, quotedPassword); err != nil {
			file.Close()
			return nil, fmt.Errorf("write temporary database client config: %w", err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close temporary database client config: %w", err)
		}
	}

	var lastErr error
	for _, binary := range []string{"mariadb", "mysql"} {
		path, err := exec.LookPath(binary)
		if err != nil {
			lastErr = err
			continue
		}
		args := make([]string, 0, 6)
		if defaultsPath != "" {
			args = append(args, "--defaults-extra-file="+defaultsPath)
		}
		args = append(args, "--protocol=socket", "--user="+username, "--batch", "--skip-column-names")
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Stdin = strings.NewReader(query)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			// Once a client has executed the operation, do not retry the same SQL
			// through another binary: some statements may have partially succeeded.
			return nil, fmt.Errorf("MariaDB operation failed: %w", err)
		}
		return stdout.Bytes(), nil
	}
	if lastErr == nil {
		lastErr = errors.New("database client is unavailable")
	}
	// Do not include client output: it may echo SQL or sensitive values.
	return nil, fmt.Errorf("MariaDB operation failed: %w", lastErr)
}

// MariaDBStringLiteral quotes a value for use in a MariaDB string literal.
func MariaDBStringLiteral(value string) (string, error) {
	if strings.IndexByte(value, 0) >= 0 {
		return "", errors.New("value contains a NUL byte")
	}
	return "CONVERT(X'" + hex.EncodeToString([]byte(value)) + "' USING utf8mb4)", nil
}

// MariaDBDump streams a consistent logical dump for the specified databases.
// It never reads the live MariaDB data directory, which would produce an
// inconsistent backup while the server is running.
func MariaDBDump(ctx context.Context, username, password string, databases []string, output io.Writer) error {
	if !validMariaDBUsername(username) || len(databases) == 0 {
		return errors.New("database backup requires a valid account and at least one database")
	}
	args := []string{"--protocol=socket", "--user=" + username, "--lock-all-tables", "--quick", "--routines", "--events", "--triggers", "--hex-blob", "--add-drop-database", "--databases"}
	for _, name := range databases {
		if !validMariaDBDatabaseName(name) {
			return errors.New("database backup contains an invalid database name")
		}
		args = append(args, name)
	}
	return runMariaDBClient(ctx, "dump", username, password, args, nil, output)
}

// MariaDBRestore streams SQL from a verified archive into the local socket.
func MariaDBRestore(ctx context.Context, username, password string, input io.Reader) error {
	if !validMariaDBUsername(username) {
		return errors.New("invalid database username")
	}
	args := []string{"--protocol=socket", "--user=" + username, "--binary-mode"}
	return runMariaDBClient(ctx, "restore", username, password, args, input, io.Discard)
}

func runMariaDBClient(ctx context.Context, operation, username, password string, baseArgs []string, input io.Reader, output io.Writer) error {
	defaultsPath, cleanup, err := createMariaDBDefaultsFile(username, password)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}
	args := append([]string(nil), baseArgs...)
	if defaultsPath != "" {
		args = append([]string{"--defaults-extra-file=" + defaultsPath}, args...)
	}
	var missingErr error
	candidates := []string{"mariadb", "mysql"}
	if operation == "dump" {
		candidates = []string{"mariadb-dump", "mysqldump"}
	}
	for _, binary := range candidates {
		path, lookErr := exec.LookPath(binary)
		if lookErr != nil {
			missingErr = lookErr
			continue
		}
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Stdin = input
		cmd.Stdout = output
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("MariaDB %s operation failed: %w", operation, err)
		}
		return nil
	}
	if missingErr == nil {
		missingErr = errors.New("database client is unavailable")
	}
	return fmt.Errorf("MariaDB %s operation failed: %w", operation, missingErr)
}

func createMariaDBDefaultsFile(username, password string) (string, func(), error) {
	if !validMariaDBUsername(username) {
		return "", nil, errors.New("invalid database username")
	}
	if password == "" {
		return "", nil, nil
	}
	if strings.ContainsAny(password, "\x00\r\n") {
		return "", nil, errors.New("database password contains unsupported control characters")
	}
	file, err := os.CreateTemp("", "megopanel-mariadb-*.cnf")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary database client config: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := file.Chmod(0600); err != nil {
		file.Close()
		cleanup()
		return "", nil, fmt.Errorf("secure temporary database client config: %w", err)
	}
	quotedPassword := strings.NewReplacer("\\", "\\\\", `"`, `\"`).Replace(password)
	if _, err := fmt.Fprintf(file, "[client]\nuser=%q\npassword=\"%s\"\n", username, quotedPassword); err != nil {
		file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temporary database client config: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary database client config: %w", err)
	}
	return path, cleanup, nil
}

func validMariaDBDatabaseName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func validMariaDBUsername(username string) bool {
	if username == "" || len(username) > 80 {
		return false
	}
	for _, r := range username {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}
