// Command otpd is the single-binary login service: it issues an SMS code,
// applies the attempt policy, and mails the decision to a compliance mailbox.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/example/sms-login-otp/internal/otp"
)

type server struct {
	client   *otp.Client
	store    *otp.Store
	auditTo  string
	nowFunc  func() time.Time
	shipName string
}

func main() {
	client, err := otp.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	auditTo := os.Getenv("AUDIT_EMAIL")
	if auditTo == "" {
		log.Fatal("AUDIT_EMAIL is not set")
	}
	s := &server{
		client:   client,
		store:    otp.NewStore(),
		auditTo:  auditTo,
		nowFunc:  time.Now,
		shipName: envOr("RELEASE_TAG", "dev"),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/login/start", s.start)
	mux.HandleFunc("/login/verify", s.verify)
	mux.HandleFunc("/login/diagnostics", s.diagnostics)

	addr := ":" + envOr("PORT", "8080")
	log.Printf("otpd %s listening on %s", s.shipName, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// start: POST {"session_id":"...","phone":"+1555..."} -> sends the code.
func (s *server) start(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		Phone     string `json:"phone"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.SessionID == "" || body.Phone == "" {
		writeJSON(w, 400, map[string]string{"error": "session_id and phone are required"})
		return
	}
	messageID, err := s.client.SendCode(body.Phone, body.SessionID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	ch := &otp.Challenge{SessionID: body.SessionID, Phone: body.Phone, MessageID: messageID, IssuedAt: s.nowFunc()}
	s.store.Put(ch)
	writeJSON(w, 202, map[string]any{
		"session_id":   ch.SessionID,
		"message_id":   messageID,
		"expires_at":   ch.IssuedAt.Add(otp.TTL).UTC().Format(time.RFC3339),
		"max_attempts": otp.MaxAttempts,
	})
}

// verify: POST {"session_id":"...","code":"123456"} -> the login decision,
// then the same decision by email so the audit trail is not a log file.
func (s *server) verify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		Code      string `json:"code"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	ch, ok := s.store.Get(body.SessionID)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "unknown session"})
		return
	}

	verified := false
	if !ch.Settled {
		var err error
		verified, err = s.client.CheckCode(ch.Phone, body.Code)
		if err != nil {
			writeAPIError(w, err)
			return
		}
	}

	now := s.nowFunc()
	outcome := otp.Evaluate(ch, now, verified)
	s.mailDecision(ch, outcome, now)
	writeJSON(w, outcome.HTTPStatus(), map[string]any{
		"session_id":     ch.SessionID,
		"outcome":        string(outcome),
		"attempts_used":  ch.Attempts,
		"attempts_limit": otp.MaxAttempts,
	})
}

// mailDecision is the handoff: the SMS result becomes an email record.
func (s *server) mailDecision(ch *otp.Challenge, outcome otp.Outcome, at time.Time) {
	line := otp.AuditLine(ch, outcome, at)
	recordID := ch.SessionID + "-" + string(outcome)
	if _, err := s.client.MailAuditRecord(
		s.auditTo,
		"login "+string(outcome)+" ("+s.shipName+")",
		line,
		recordID,
	); err != nil {
		log.Printf("audit mail deferred for %s: %v", ch.SessionID, err)
		return
	}
	log.Printf("audit mailed: %s", line)
}

// diagnostics: GET /login/diagnostics?session_id=... — where the code message got to.
func (s *server) diagnostics(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.store.Get(r.URL.Query().Get("session_id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "unknown session"})
		return
	}
	status, err := s.client.DeliveryStatus(ch.MessageID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"session_id":    ch.SessionID,
		"message_id":    ch.MessageID,
		"delivery":      status,
		"attempts_used": ch.Attempts,
		"settled":       ch.Settled,
		"release":       s.shipName,
	})
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "use POST"})
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

// An Infrai envelope that reports a rejected argument is an answer for our
// caller too, so it travels back as a 400 rather than a server error.
func writeAPIError(w http.ResponseWriter, err error) {
	if apiErr, ok := err.(*otp.APIError); ok {
		writeJSON(w, 400, map[string]string{"error": apiErr.Code, "detail": apiErr.Hint})
		return
	}
	writeJSON(w, 502, map[string]string{"error": "upstream unreachable"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
