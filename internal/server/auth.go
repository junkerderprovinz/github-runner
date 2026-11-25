package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	cookieName  = "github_runner_session"
	sessionLife = 30 * 24 * time.Hour
	failWindow  = 15 * time.Minute
	freeFails   = 3
	maxDelay    = 10 * time.Second
)

// The parameters are OWASP's smallest recommended set for argon2id. A sign-in
// is rare here, so there is no reason to go lower.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

func hashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads, b64(salt), b64(key))
}

func checkPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// sessions lives in memory, so a restart of the container signs everyone out.
type sessions struct {
	mu     sync.Mutex
	now    func() time.Time
	open   map[string]time.Time
	fails  map[string][]time.Time
	checks map[string]*sync.Mutex
}

func newSessions() *sessions {
	return &sessions{now: time.Now, open: map[string]time.Time{}, fails: map[string][]time.Time{}, checks: map[string]*sync.Mutex{}}
}

func (s *sessions) start(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 32)
	rand.Read(b)
	id := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	s.open[id] = s.now().Add(sessionLife)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: id, Path: "/", MaxAge: int(sessionLife.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: overTLS(r),
	})
}

func (s *sessions) valid(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.open[c.Value]
	if ok && s.now().After(until) {
		delete(s.open, c.Value)
		return false
	}
	return ok
}

func (s *sessions) end(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.open, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: overTLS(r),
	})
}

// endAll signs everyone out, after the password changed.
func (s *sessions) endAll() {
	s.mu.Lock()
	clear(s.open)
	s.mu.Unlock()
}

// checking lets one password check run at a time per client, so sending many
// in parallel does not get around the delay.
func (s *sessions) checking(client string) func() {
	s.mu.Lock()
	m := s.checks[client]
	if m == nil {
		m = &sync.Mutex{}
		s.checks[client] = m
	}
	s.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// failed records a wrong password and returns how long to wait before
// answering. The right password is never held back, so nobody can lock the
// owner out by guessing.
func (s *sessions) failed(client string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	fails := append(s.recentFails(client), s.now())
	s.fails[client] = fails
	if len(fails) < freeFails {
		return 0
	}
	return min(time.Second<<(len(fails)-freeFails), maxDelay)
}

func (s *sessions) forgive(client string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.fails, client)
}

// recentFails drops failures outside the window. The caller holds s.mu.
func (s *sessions) recentFails(ip string) []time.Time {
	cut := s.now().Add(-failWindow)
	var recent []time.Time
	for _, t := range s.fails[ip] {
		if t.After(cut) {
			recent = append(recent, t)
		}
	}
	if recent == nil {
		delete(s.fails, ip)
	} else {
		s.fails[ip] = recent
	}
	return recent
}

// clientOf names who is signing in. Behind a reverse proxy in the own network
// every visitor arrives from the proxy, so the address it forwards counts.
func clientOf(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if ip := net.ParseIP(peer); ip == nil || !(ip.IsLoopback() || ip.IsPrivate()) {
		return peer
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(forwarded) - 1; i >= 0; i-- {
		addr := strings.TrimSpace(forwarded[i])
		if ip := net.ParseIP(addr); ip != nil && !ip.IsLoopback() && !ip.IsPrivate() {
			return addr
		}
	}
	return peer
}

// homeHost reports whether the page was opened by an address of the own
// network. Without a password only those are answered, so a website cannot
// reach the API through DNS rebinding.
func homeHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if net.ParseIP(host) != nil || host == "localhost" || !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range []string{".local", ".lan", ".home.arpa", ".internal"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func overTLS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}
