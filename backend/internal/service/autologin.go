package service

import (
	"errors"
	"sync"
	"time"
)

type PMAAutologinToken struct {
	UserID        string    `json:"user_id"`
	MySQLUsername string    `json:"mysql_username"`
	MySQLPassword string    `json:"mysql_password"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type AutologinStore struct {
	mu     sync.RWMutex
	tokens map[string]*PMAAutologinToken
}

func NewAutologinStore() *AutologinStore {
	store := &AutologinStore{
		tokens: make(map[string]*PMAAutologinToken),
	}
	go store.cleanupPeriodically()
	return store
}

func (s *AutologinStore) Save(token string, pmaToken *PMAAutologinToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = pmaToken
}

func (s *AutologinStore) RetrieveAndConsume(token string) (*PMAAutologinToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pmaToken, exists := s.tokens[token]
	if !exists {
		return nil, errors.New("token not found or already used")
	}

	// Delete immediately to guarantee single-use and avoid reuse or race conditions
	delete(s.tokens, token)

	if time.Now().After(pmaToken.ExpiresAt) {
		return nil, errors.New("token expired")
	}

	return pmaToken, nil
}

func (s *AutologinStore) cleanupPeriodically() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for token, pmaToken := range s.tokens {
			if now.After(pmaToken.ExpiresAt) {
				delete(s.tokens, token)
			}
		}
		s.mu.Unlock()
	}
}
