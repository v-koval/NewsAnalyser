package auth

import (
	"sync"
	"time"
)

const (
	limiterThreshold  = 5
	limiterBaseBlock  = time.Minute
	limiterMaxBlock   = 15 * time.Minute
	limiterStaleAfter = 30 * time.Minute
)

type limiterEntry struct {
	fails        int
	blockDur     time.Duration
	blockedUntil time.Time
	lastFail     time.Time
}

// LoginLimiter is an in-memory brute-force limiter keyed by "IP|email".
// After limiterThreshold consecutive failures the key is blocked for
// limiterBaseBlock; every further failure doubles the block up to
// limiterMaxBlock. A successful login resets the key.
type LoginLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	now     func() time.Time
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{entries: map[string]*limiterEntry{}, now: time.Now}
}

// Allowed reports whether an attempt may proceed; when blocked it also
// returns the remaining block duration.
func (l *LoginLimiter) Allowed(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gc()
	e := l.entries[key]
	if e == nil {
		return true, 0
	}
	if now := l.now(); now.Before(e.blockedUntil) {
		return false, e.blockedUntil.Sub(now)
	}
	return true, 0
}

func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil {
		e = &limiterEntry{}
		l.entries[key] = e
	}
	e.fails++
	e.lastFail = l.now()
	if e.fails < limiterThreshold {
		return
	}
	if e.blockDur == 0 {
		e.blockDur = limiterBaseBlock
	} else {
		e.blockDur *= 2
		if e.blockDur > limiterMaxBlock {
			e.blockDur = limiterMaxBlock
		}
	}
	e.blockedUntil = l.now().Add(e.blockDur)
}

func (l *LoginLimiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// gc drops entries that are unblocked and stale. Caller must hold l.mu.
func (l *LoginLimiter) gc() {
	now := l.now()
	for k, e := range l.entries {
		if now.After(e.blockedUntil) && now.Sub(e.lastFail) > limiterStaleAfter {
			delete(l.entries, k)
		}
	}
}
