package otp

import (
	"strings"
	"testing"
	"time"
)

var issued = time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)

func TestEvaluateAttemptPolicy(t *testing.T) {
	cases := []struct {
		name     string
		attempts int
		settled  bool
		elapsed  time.Duration
		verified bool
		want     Outcome
		wantCode int
	}{
		{"correct code on first try", 0, false, time.Minute, true, Granted, 200},
		{"wrong code, tries left", 1, false, time.Minute, false, Retry, 401},
		{"wrong code burns the last attempt", 4, false, time.Minute, false, Locked, 423},
		{"correct code on the last attempt still wins", 4, false, time.Minute, true, Granted, 200},
		{"code arrives after the window closed", 0, false, TTL, true, Expired, 410},
		{"a settled challenge cannot be reopened", 5, true, time.Minute, true, Locked, 423},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := &Challenge{SessionID: "s-1", Phone: "+15551234567", IssuedAt: issued, Attempts: c.attempts, Settled: c.settled}
			got := Evaluate(ch, issued.Add(c.elapsed), c.verified)
			if got != c.want {
				t.Fatalf("outcome = %q, want %q", got, c.want)
			}
			if code := got.HTTPStatus(); code != c.wantCode {
				t.Fatalf("status = %d, want %d", code, c.wantCode)
			}
		})
	}
}

func TestAuditLineMasksTheNumber(t *testing.T) {
	ch := &Challenge{SessionID: "s-9", Phone: "+15551234567", IssuedAt: issued, Attempts: 2}
	line := AuditLine(ch, Retry, issued.Add(30*time.Second))
	if strings.Contains(line, "+1555123") {
		t.Fatalf("audit line leaked the full number: %s", line)
	}
	if !strings.Contains(line, "*4567") || !strings.Contains(line, "outcome=retry") {
		t.Fatalf("audit line missing masked tail or outcome: %s", line)
	}
}
