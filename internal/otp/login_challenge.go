package otp

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// MaxAttempts and TTL are the login policy a fintech reviewer will ask about
// first: how many guesses, and for how long.
const (
	MaxAttempts = 5
	TTL         = 10 * time.Minute
)

// Outcome is the state transition a single code submission produces.
type Outcome string

const (
	Granted Outcome = "granted"
	Retry   Outcome = "retry"
	Locked  Outcome = "locked"
	Expired Outcome = "expired"
)

// Challenge is one in-flight login for one phone number.
type Challenge struct {
	SessionID string
	Phone     string
	MessageID string
	IssuedAt  time.Time
	Attempts  int
	Settled   bool
}

// Evaluate applies the attempt policy to a verification result. It is pure:
// no network, no clock reads, so the login rules can be tested directly.
// The caller supplies `verified` from sms.verify; everything else is policy.
func Evaluate(ch *Challenge, now time.Time, verified bool) Outcome {
	if ch.Settled {
		return Locked
	}
	if now.Sub(ch.IssuedAt) >= TTL {
		ch.Settled = true
		return Expired
	}
	ch.Attempts++
	if verified {
		ch.Settled = true
		return Granted
	}
	if ch.Attempts >= MaxAttempts {
		ch.Settled = true
		return Locked
	}
	return Retry
}

// HTTPStatus maps an outcome to the status this service returns to its own
// caller. A refused code is a client-side answer, never a server fault.
func (o Outcome) HTTPStatus() int {
	switch o {
	case Granted:
		return 200
	case Retry:
		return 401
	case Locked:
		return 423
	default:
		return 410
	}
}

// AuditLine is the one-line record the compliance mailbox receives.
func AuditLine(ch *Challenge, o Outcome, at time.Time) string {
	return fmt.Sprintf(
		"session=%s phone=%s outcome=%s attempts=%d/%d issued=%s decided=%s",
		ch.SessionID, maskPhone(ch.Phone), o, ch.Attempts, MaxAttempts,
		ch.IssuedAt.UTC().Format(time.RFC3339), at.UTC().Format(time.RFC3339),
	)
}

func maskPhone(p string) string {
	if len(p) <= 4 {
		return strings.Repeat("*", len(p))
	}
	return strings.Repeat("*", len(p)-4) + p[len(p)-4:]
}

// Store keeps live challenges in memory — one process, one binary.
type Store struct {
	mu   sync.Mutex
	byID map[string]*Challenge
}

func NewStore() *Store { return &Store{byID: map[string]*Challenge{}} }

func (s *Store) Put(ch *Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[ch.SessionID] = ch
}

func (s *Store) Get(sessionID string) (*Challenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.byID[sessionID]
	return ch, ok
}
