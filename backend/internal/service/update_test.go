package service

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current  string
		latest   string
		expected bool
	}{
		{"v1.1.2", "1.1.2", false},
		{"v1.1.2", "v1.1.2", false},
		{"1.1.2", "1.1.2", false},
		{"v1.0.0", "1.1.2", true},
		{"1.0.0", "v1.1.2", true},
		{"v1.1.2", "1.0.0", false},
		{"v1.1.2", "v1.2.0", true},
		{"v1.1.2", "v1.1.2-rc.1", false},
		{"v1.1.2-rc.1", "v1.1.2", true},
		{"v1.1.2", "latest", false},
	}

	for _, tt := range tests {
		result := isNewerVersion(tt.current, tt.latest)
		if result != tt.expected {
			t.Errorf("isNewerVersion(%q, %q) = %v; expected %v", tt.current, tt.latest, result, tt.expected)
		}
	}
}

func TestCleanVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"v1.1.2", "1.1.2"},
		{"V1.1.2", "1.1.2"},
		{" 1.1.2 ", "1.1.2"},
		{"  v1.1.2  ", "1.1.2"},
	}

	for _, tt := range tests {
		result := cleanVersion(tt.input)
		if result != tt.expected {
			t.Errorf("cleanVersion(%q) = %q; expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsStableVersion(t *testing.T) {
	for _, test := range []struct {
		version string
		stable  bool
	}{
		{version: "v1.2.3", stable: true},
		{version: "1.2.3", stable: true},
		{version: "v1.2.3-rc.1", stable: false},
		{version: "latest", stable: false},
	} {
		if got := isStableVersion(test.version); got != test.stable {
			t.Errorf("isStableVersion(%q) = %v, want %v", test.version, got, test.stable)
		}
	}
}

func TestUntarRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "unsafe.tar.gz")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(archiveFile)
	writer := tar.NewWriter(compressed)
	contents := []byte("outside")
	if err := writer.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0600, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatal(err)
	}
	extractDir := filepath.Join(root, "extract")
	if err := os.Mkdir(extractDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := untar(archivePath, extractDir); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
	if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("archive wrote a file outside its extraction directory")
	}
}
