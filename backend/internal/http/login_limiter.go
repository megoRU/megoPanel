package http

import (
	"sync"
	"time"
)

type loginAttempt struct {
	windowStart  time.Time
	blockedUntil time.Time
	failures     int
}

type loginLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string]loginAttempt
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: window, attempts: make(map[string]loginAttempt)}
}

func (l *loginLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanup(now)
	state, exists := l.attempts[key]
	if exists && now.Before(state.blockedUntil) {
		return false, state.blockedUntil.Sub(now)
	}
	if !exists || now.Sub(state.windowStart) >= l.window || now.Before(state.windowStart) {
		l.attempts[key] = loginAttempt{windowStart: now}
	}
	return true, 0
}

func (l *loginLimiter) failed(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, exists := l.attempts[key]
	if !exists || now.Sub(state.windowStart) >= l.window || now.Before(state.windowStart) {
		state = loginAttempt{windowStart: now}
	}
	state.failures++
	if state.failures >= l.max {
		state.blockedUntil = now.Add(l.window)
	}
	l.attempts[key] = state
	l.cleanup(now)
}

func (l *loginLimiter) succeeded(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func (l *loginLimiter) cleanup(now time.Time) {
	for key, state := range l.attempts {
		if !now.Before(state.blockedUntil) && now.Sub(state.windowStart) >= 2*l.window {
			delete(l.attempts, key)
		}
	}
}
