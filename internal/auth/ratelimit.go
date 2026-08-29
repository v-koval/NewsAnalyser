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
	limiterMaxEntries = 10000
	limiterGCInterval = time.Minute
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
// Escalation state survives block expiry — after a block ends, the next
// single failure re-blocks with a doubled duration; only Success resets the
// key. The map is capped at limiterMaxEntries; when full, an unblocked entry
// is evicted first.
type LoginLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	now     func() time.Time
	lastGC  time.Time
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
		if len(l.entries) >= limiterMaxEntries {
			l.sweep()
			if len(l.entries) >= limiterMaxEntries {
				l.evictOne()
			}
		}
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

// gc runs sweep at most once per limiterGCInterval. Caller must hold l.mu.
func (l *LoginLimiter) gc() {
	if l.now().Sub(l.lastGC) < limiterGCInterval {
		return
	}
	l.lastGC = l.now()
	l.sweep()
}

// sweep drops entries that are unblocked and stale. Caller must hold l.mu.
func (l *LoginLimiter) sweep() {
	now := l.now()
	for k, e := range l.entries {
		if now.After(e.blockedUntil) && now.Sub(e.lastFail) > limiterStaleAfter {
			delete(l.entries, k)
		}
	}
}

// evictOne makes room when the map is at capacity: prefer any unblocked
// entry, fall back to an arbitrary one. Caller must hold l.mu.
func (l *LoginLimiter) evictOne() {
	now := l.now()
	fallback := ""
	for k, e := range l.entries {
		if now.After(e.blockedUntil) {
			delete(l.entries, k)
			return
		}
		fallback = k
	}
	if fallback != "" {
		delete(l.entries, fallback)
	}
}
