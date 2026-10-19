package service

import (
	"strings"
	"testing"
)

func TestSignonTemplateCarriesTokenInFragmentAndPostsIt(t *testing.T) {
	signon := getSignonPHP("8765")
	for _, expected := range []string{
		"MegoPanel sign-on bridge v2",
		"location.hash.slice(1)",
		"method: 'POST'",
		"/internal/phpmyadmin/token",
		"__MEGOPANEL_API_PORT__",
	} {
		if strings.Contains(expected, "__MEGOPANEL_API_PORT__") {
			continue
		}
		if !strings.Contains(signon, expected) {
			t.Errorf("sign-on page is missing %q", expected)
		}
	}
	if strings.Contains(signon, "$_GET['token']") || strings.Contains(signon, "token=") {
		t.Fatal("sign-on token must not appear in query strings")
	}
	if !strings.Contains(signon, "127.0.0.1:8765/internal/phpmyadmin/token") {
		t.Fatal("API port placeholder was not substituted")
	}
}
