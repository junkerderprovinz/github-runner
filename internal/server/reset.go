package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/github-runner/internal/config"
	"github.com/junkerderprovinz/github-runner/internal/runner"
)

// A reset code is twelve characters, about 60 bits, from an alphabet without
// 0, O, 1 and I. It only ever appears in the container log, because whoever
// reads that log runs the container anyway.
const (
	resetAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	resetLen      = 12
	resetLife     = 10 * time.Minute
	resetNoteGap  = time.Minute
)

const resetRefused = "That code is wrong or has expired. If it expired, the log now shows a new one."

type resetCode struct {
	mu sync.Mutex
	resetState
}

type resetState struct {
	digest  [sha256.Size]byte
	expires time.Time
	refused int
	noted   time.Time
}

func normalizeCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(code))
}

// requestReset writes a code to the container log. Asking again while one is
// valid writes nothing, so the code somebody is typing stays good.
func (s *Server) requestReset(w http.ResponseWriter, r *http.Request) {
	if !s.locked() {
		fail(w, http.StatusConflict, "No password is set, so there is nothing to reset.")
		return
	}
	now := s.now()
	rc := &s.reset
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if now.Before(rc.expires) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	b := make([]byte, resetLen)
	rand.Read(b)
	for i := range b {
		b[i] = resetAlphabet[b[i]%byte(len(resetAlphabet))]
	}
	code := string(b)
	rc.resetState = resetState{digest: sha256.Sum256(b), expires: now.Add(resetLife)}
	frame := strings.Repeat("=", 76)
	s.sup.Print("", frame,
		"  Someone asked to reset the password of the web interface.",
		"  Password reset code: "+code[:4]+"-"+code[4:8]+"-"+code[8:],
		"  It is valid for 10 minutes.",
		"  If nobody here asked for a reset, ignore this. The password is unchanged.",
		frame, "")
	w.WriteHeader(http.StatusNoContent)
}

// resetPassword sets a new password with the code from the log. A wrong code
// does not spend the right one, since a stranger could otherwise keep the
// owner from ever resetting.
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if !read(w, r, &body) {
		return
	}
	if body.Password == "" {
		fail(w, http.StatusBadRequest, "Type a new password first.")
		return
	}
	client := "reset " + clientOf(r)
	done := s.sessions.checking(client)
	defer done()

	typed := sha256.Sum256([]byte(normalizeCode(body.Code)))
	now := s.now()
	rc := &s.reset
	rc.mu.Lock()
	good := now.Before(rc.expires) && subtle.ConstantTimeCompare(typed[:], rc.digest[:]) == 1
	if !good {
		rc.refused++
		if now.Sub(rc.noted) >= resetNoteGap {
			s.sup.Say("Wrong password reset codes typed so far: %d.", rc.refused)
			rc.noted = now
		}
		rc.mu.Unlock()
		if wait := s.sessions.failed(client); wait > 0 {
			s.sleep(wait)
		}
		fail(w, http.StatusUnauthorized, resetRefused)
		return
	}
	defer rc.mu.Unlock()
	hash := hashPassword(body.Password)
	if err := s.cfg.Update(func(c *config.Config) error { c.PasswordHash = hash; return nil }); err != nil {
		fail(w, http.StatusInternalServerError, "The password could not be saved: "+err.Error())
		return
	}
	rc.resetState = resetState{}
	s.sessions.forgive(client)
	s.sessions.endAll()
	s.sessions.start(w, r)
	s.sup.Say("The password was reset with the code from the log. Every other session has ended.")
	s.sup.Emit(runner.Event{Type: "settings", Data: viewSettings(s.cfg.Get())})
	w.WriteHeader(http.StatusNoContent)
}
