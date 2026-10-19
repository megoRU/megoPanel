package platform

import "testing"

func TestMariaDBStringLiteralEscapesSQLCharacters(t *testing.T) {
	got, err := MariaDBStringLiteral("quote' slash\\ newline\n")
	if err != nil {
		t.Fatalf("MariaDBStringLiteral returned an error: %v", err)
	}
	want := "CONVERT(X'71756f74652720736c6173685c206e65776c696e650a' USING utf8mb4)"
	if got != want {
		t.Fatalf("unexpected quoted string\n got: %q\nwant: %q", got, want)
	}
}

func TestMariaDBStringLiteralRejectsNUL(t *testing.T) {
	if _, err := MariaDBStringLiteral("bad\x00value"); err == nil {
		t.Fatal("expected NUL byte to be rejected")
	}
}
