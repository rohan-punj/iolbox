package main

import (
	"net/http"
	"sync"
	"time"
)

// Simple in-memory session store. Every page except the login form and
// static assets requires a valid session cookie — this GUI can trigger real
// L2/L3 attacks, so it is guarded like every other node in this repo.
type sessions struct {
	mu  sync.Mutex
	tok map[string]time.Time
}

func newSessions() *sessions { return &sessions{tok: map[string]time.Time{}} }

const sessionTTL = 8 * time.Hour
const cookieName = "secbench_session"

func (s *sessions) create() string {
	t := randHex(24)
	s.mu.Lock()
	s.tok[t] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return t
}

func (s *sessions) valid(t string) bool {
	if t == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tok[t]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.tok, t)
		return false
	}
	return true
}

func (s *sessions) destroy(t string) {
	s.mu.Lock()
	delete(s.tok, t)
	s.mu.Unlock()
}

func (s *sessions) authed(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	return s.valid(c.Value)
}
