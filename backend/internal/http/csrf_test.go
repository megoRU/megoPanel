package http

import "testing"

func TestCSRFCookieRequiresValidSignature(t *testing.T) {
	secret := "unit-test-csrf-secret-with-more-than-32-bytes"
	cookie, err := newCSRFCookie(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !validCSRFCookie(cookie, secret) {
		t.Fatal("new CSRF cookie should validate")
	}
	if validCSRFCookie(cookie, secret+"wrong") {
		t.Fatal("CSRF cookie signed with another secret must be rejected")
	}
	tampered := cookie[:64] + "."
	if cookie[65] == '0' {
		tampered += "1" + cookie[66:]
	} else {
		tampered += "0" + cookie[66:]
	}
	if validCSRFCookie(tampered, secret) {
		t.Fatal("modified CSRF signature must be rejected")
	}
}
