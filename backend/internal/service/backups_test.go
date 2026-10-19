package service

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testBackupID = "123e4567-e89b-12d3-a456-426614174000"

func TestValidBackupEntryName(t *testing.T) {
	for _, name := range []string{"manifest.json", "panel.db", "websites/", "websites/example.com/index.html", "websites/example.com/assets/"} {
		if !validBackupEntryName(name) {
			t.Errorf("expected %q to be allowed", name)
		}
	}
	for _, name := range []string{"../panel.db", "/etc/passwd", "websites/../panel.db", "websites/example.com/../../etc/passwd", "websites\\example.com\\index.html", "websites//index.html", "websites/./index.html", "websites/phpmyadmin/config.inc.php"} {
		if validBackupEntryName(name) {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestVerifyBackupArchive(t *testing.T) {
	tests := []struct {
		name       string
		entries    []backupTestEntry
		wantErr    bool
	}{
		{
			name: "valid panel backup",
			entries: []backupTestEntry{
				{name: "manifest.json", data: testPanelManifest(t)},
				{name: "panel.db", data: []byte("sqlite bytes")},
				{name: "config.yaml", data: []byte("config: test")},
			},
		},
		{
			name: "rejects path traversal",
			entries: []backupTestEntry{
				{name: "manifest.json", data: testPanelManifest(t)},
				{name: "panel.db", data: []byte("sqlite bytes")},
				{name: "config.yaml", data: []byte("config: test")},
				{name: "websites/../../escape", data: []byte("outside")},
			},
			wantErr: true,
		},
		{
			name: "rejects a symlink",
			entries: []backupTestEntry{
				{name: "manifest.json", data: testPanelManifest(t)},
				{name: "panel.db", data: []byte("sqlite bytes"), mode: os.ModeSymlink | 0777},
				{name: "config.yaml", data: []byte("config: test")},
			},
			wantErr: true,
		},
		{
			name: "requires declared components",
			entries: []backupTestEntry{
				{name: "manifest.json", data: testPanelManifest(t)},
				{name: "config.yaml", data: []byte("config: test")},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestBackup(t, test.entries)
			err := verifyBackupArchive(path)
			if (err != nil) != test.wantErr {
				t.Fatalf("verifyBackupArchive() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestVerifyBackupArchiveRejectsDuplicatePaths(t *testing.T) {
	entries := []backupTestEntry{
		{name: "manifest.json", data: testPanelManifest(t)},
		{name: "panel.db", data: []byte("first")},
		{name: "panel.db", data: []byte("second")},
		{name: "config.yaml", data: []byte("config: test")},
	}
	if err := verifyBackupArchive(writeTestBackup(t, entries)); err == nil {
		t.Fatal("expected duplicate zip paths to be rejected")
	}
}

func TestValidateBackupSettings(t *testing.T) {
	valid := BackupSettings{Directory: defaultBackupDir, ScheduleHour: 3, RetentionCount: 10, RetentionDays: 30, ScheduleComponents: []string{"panel", "mariadb"}}
	if err := validateBackupSettings(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	valid.Directory = "/etc"
	if err := validateBackupSettings(valid); err == nil {
		t.Fatal("expected an unsupported backup path to be rejected")
	}
}

type backupTestEntry struct {
	name string
	data []byte
	mode os.FileMode
}

func testPanelManifest(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(BackupManifest{
		FormatVersion: backupFormatVersion,
		ID:            testBackupID,
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		AppVersion:    CurrentVersion,
		Components:    []string{"panel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeTestBackup(t *testing.T, entries []backupTestEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), testBackupID+".zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		mode := entry.mode
		if mode == 0 {
			mode = 0600
		}
		header.SetMode(mode)
		stream, err := writer.CreateHeader(header)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		if _, err := stream.Write(entry.data); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	checksum, err := hashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeChecksumFile(path, checksum); err != nil {
		t.Fatal(err)
	}
	return path
}
