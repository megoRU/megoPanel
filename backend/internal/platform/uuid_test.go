package platform

import (
	"strings"
	"testing"
)

func TestNewUUIDv7(t *testing.T) {
	uuid, err := NewUUIDv7()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(uuid) != 36 {
		t.Errorf("expected UUIDv7 length to be 36 characters, got %d", len(uuid))
	}

	hyphens := strings.Count(uuid, "-")
	if hyphens != 4 {
		t.Errorf("expected 4 hyphens, got %d in %s", hyphens, uuid)
	}

	// UUIDv7 version is at index 14
	// Format: xxxxxxxx-xxxx-7xxx-xxxx-xxxxxxxxxxxx
	versionChar := string(uuid[14])
	if versionChar != "7" {
		t.Errorf("expected version character '7', got %s in %s", versionChar, uuid)
	}
}
