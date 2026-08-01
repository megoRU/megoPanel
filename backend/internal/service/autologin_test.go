package service

import (
	"testing"
	"time"
)

func TestAutologinStore_Save_And_Retrieve(t *testing.T) {
	store := NewAutologinStore()
	token := "test-token"
	pmaToken := &PMAAutologinToken{
		UserID:        "1",
		MySQLUsername: "root",
		MySQLPassword: "password",
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(15 * time.Second),
	}

	store.Save(token, pmaToken)

	retrieved, err := store.RetrieveAndConsume(token)
	if err != nil {
		t.Fatalf("expected to successfully retrieve token, got %v", err)
	}

	if retrieved.UserID != pmaToken.UserID || retrieved.MySQLUsername != pmaToken.MySQLUsername {
		t.Errorf("retrieved token data mismatch")
	}

	// Single-use token check: must fail on second consumption
	_, err = store.RetrieveAndConsume(token)
	if err == nil {
		t.Fatalf("expected error on double-retrieving the same single-use token")
	}
}

func TestAutologinStore_ExpiredToken(t *testing.T) {
	store := NewAutologinStore()
	token := "expired-token"
	pmaToken := &PMAAutologinToken{
		UserID:        "1",
		MySQLUsername: "root",
		MySQLPassword: "password",
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(-1 * time.Second), // expired
	}

	store.Save(token, pmaToken)

	_, err := store.RetrieveAndConsume(token)
	if err == nil {
		t.Fatalf("expected error because the token is expired")
	}
}
