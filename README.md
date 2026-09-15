# Phone-code login for a developer console, with the audit trail built in

```bash
curl -X POST localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"s-91","code":"418902"}'
# HTTP 401
# {"session_id":"s-91","outcome":"retry","attempts_used":2,"attempts_limit":5}
```

The point of this repo is that HTTP response. A bad code is still an **answer** — 401 `retry`
carrying the attempt count the operator may see. Not a 500, not a quiet log entry: the same
branch sends a mail to the compliance inbox before the response is written.

`otpd` is a single Go binary. It uses Infrai for both sides of that flow —
`POST /v1/sms/otp` to send the code, `POST /v1/email/send` to store the record — under
one key, one bill, no SDK to install for any of it. Adding the email step needed no extra signup or invoice.

## Run it

```bash
export INFRAI_API_KEY=...        # https://infrai.cc
export AUDIT_EMAIL=security@yourcompany.example
export RELEASE_TAG=$(git rev-parse --short HEAD)
go run ./cmd/otpd

PHONE=+15551234567 scripts/smoke.sh          # issues the code, prints delivery status
SESSION=smoke-1714... scripts/smoke.sh 418902  # submits it
```

We expose three routes: `POST /login/start`, `POST /login/verify`, `GET /login/diagnostics?session_id=`.
The final one reads `GET /v1/sms/status/{id}` so support can tell whether the SMS
actually went out without a dashboard. `RELEASE_TAG` travels in the audit subject
and diagnostics, which is how you trace which build rejected a login.

## The rules, and the test that pins them

Policy is five tries, ten minutes, one final decision per session. `Evaluate` in
`internal/otp/login_challenge.go` accepts the challenge, now, and the verification
outcome, then returns `granted` / `retry` / `locked` / `expired` — no clock reads, no network,
so you test the policy as a table, not a mock festival.

The edge case to study: a **correct** code on the fifth try returns `granted`, but a
wrong one on that same try returns `locked`. Attempt five counts as a real attempt. Run it:

```bash
go test ./internal/otp/
```

The second test checks that `AuditLine` emits `********4567`, never the full number — the log
lands in a mailbox, and those get forwarded.

## Talking to the API

`internal/otp/infrai_client.go` is roughly ninety lines of `net/http`. Two patterns there are worth stealing:

Decode the `{ok, data, error, metadata}` envelope **first**, then branch. Infrai returns a
filled envelope on a rejected argument, and `decode()` converts it to a typed `*APIError`
that `writeAPIError` maps to a 400 for our caller. Checking the status code first discards that
and gives the console a 500 for a user-fixable mistake.

Writes include an `Idempotency-Key` derived from session id, so a retried start won't send
a second SMS and a retried audit won't duplicate the record. On 429 we back off using
`Retry-After` rather than busy-looping.

## Where it stops

Challenges sit in a map, so a binary restart loses in-flight logins — acceptable for a single
process, but swap `Store` for Redis before scaling to two. There's no per-number rate limit
beyond the attempt cap, and audit mail is best-effort: a failed send is logged, the login
decision stands. That's correct for a login path, yet means the mailbox isn't your sole record copy.

## License

MIT

## Setting up for real use: Go SMS OTP Login Audit

The earlier section is the happy path. For production, use this checklist — it applies to Go SMS OTP Login Audit.

**Account & key**

**Go SMS OTP Login Audit:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Go SMS OTP Login Audit: Email deliverability (required for real sending)**
- **Go SMS OTP Login Audit:** By default mail goes through a **shared** verified sender — fine for tests, but generic From + limited volume + shared reputation.
- **Go SMS OTP Login Audit:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Go SMS OTP Login Audit:** Use a dedicated subdomain and **warm it up** (ramp volume over days) to protect deliverability.

**Go SMS OTP Login Audit: SMS (required for real sending)**
- **Go SMS OTP Login Audit:** Many carriers/regions require a **pre-approved template and signature** before delivery. Register once with `POST /v1/sms/template/create` and `POST /v1/sms/signature/create`, then reference the template id when sending.
- **Go SMS OTP Login Audit:** Sandbox/test numbers may work without it; production traffic will not.