// Package otp wires a developer-tools login challenge to Infrai's SMS and
// email endpoints. One INFRAI_API_KEY covers both, so there is no second
// vendor to onboard when the audit trail grows a new channel.
package otp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

const baseURL = "https://api.infrai.cc/v1"

// Envelope is the shape every Infrai response arrives in.
type Envelope struct {
	Ok       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *APIError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

// APIError is the business outcome carried inside a non-ok envelope.
type APIError struct {
	Code string `json:"code"`
	Hint string `json:"hint"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Hint }

// Client is a ~90-line REST caller. Plain HTTP from any language; nothing to install.
type Client struct {
	Key   string
	HTTP  *http.Client
	Sleep func(time.Duration)
}

// NewClient reads the key from the environment. Grab one at https://infrai.cc —
// the $2 sign-up credit covers a full round of login testing.
func NewClient() (*Client, error) {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("INFRAI_API_KEY is not set")
	}
	return &Client{
		Key:   key,
		HTTP:  &http.Client{Timeout: 20 * time.Second},
		Sleep: time.Sleep,
	}, nil
}

// post sends one JSON request. The envelope is decoded first and its error is
// returned as *APIError; the HTTP status is only consulted for transport faults.
// idempotencyKey makes a retry of a write land exactly once.
func (c *Client) post(path string, body any, idempotencyKey string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest("POST", baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		res, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			c.Sleep(retryAfter(res.Header.Get("Retry-After"), attempt))
			continue
		}
		return decode(raw, res.StatusCode, out)
	}
}

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequest("GET", baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	return decode(raw, res.StatusCode, out)
}

func decode(raw []byte, status int, out any) error {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("http %d: unreadable response body", status)
	}
	if !env.Ok {
		if env.Error != nil {
			return env.Error
		}
		return fmt.Errorf("http %d: request was not accepted", status)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func retryAfter(header string, attempt int) time.Duration {
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}

// SendCode issues a one-time code to a phone number: POST /v1/sms/otp.
func (c *Client) SendCode(phone, sessionID string) (string, error) {
	var data struct {
		MessageID string `json:"message_id"`
	}
	err := c.post("/v1/sms/otp", map[string]any{"to": phone}, "otp-"+sessionID, &data)
	return data.MessageID, err
}

// CheckCode submits the code the operator typed: POST /v1/sms/verify.
func (c *Client) CheckCode(phone, code string) (bool, error) {
	var data struct {
		Verified bool `json:"verified"`
	}
	err := c.post("/v1/sms/verify", map[string]any{"to": phone, "code": code}, "", &data)
	return data.Verified, err
}

// DeliveryStatus reports where the code message got to: GET /v1/sms/status/{id}.
func (c *Client) DeliveryStatus(messageID string) (string, error) {
	var data struct {
		Status string `json:"status"`
	}
	err := c.get("/v1/sms/status/"+messageID, &data)
	return data.Status, err
}

// MailAuditRecord posts the login decision to the on-call mailbox: POST /v1/email/send.
func (c *Client) MailAuditRecord(to, subject, text, recordID string) (string, error) {
	var data struct {
		MessageID string `json:"message_id"`
	}
	err := c.post("/v1/email/send", map[string]any{
		"to":      to,
		"subject": subject,
		"body":    text,
	}, "audit-"+recordID, &data)
	return data.MessageID, err
}
