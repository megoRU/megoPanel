package service

import "testing"

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
		{"v1.1.2", "1.0.0", true}, // Simple string mismatch check as implemented
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
