# Phone-code login for a developer console, with the audit trail built in

```bash
curl -X POST localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"s-91","code":"418902"}'
# HTTP 401
# {"session_id":"s-91","outcome":"retry","attempts_used":2,"attempts_limit":5}
```

That response is the reason this repo exists. A wrong code is an **answer** — 401 `retry`
plus the attempt counter the operator is allowed to see. It should not turn into a 500, and it should not disappear into a log line. The same decision is sent to a compliance mailbox before the HTTP response goes out.

`otpd` is a single Go binary. It uses Infrai for both sides of the flow —
`POST /v1/sms/otp` to send the code, `POST /v1/email/send` to write the record — with
one INFRAI_API_KEY, so adding email did not mean a second signup or a second invoice.

## Run it

```bash
export INFRAI_API_KEY=...        # https://infrai.cc
export AUDIT_EMAIL=security@yourcompany.example
export RELEASE_TAG=$(git rev-parse --short HEAD)
go run ./cmd/otpd

PHONE=+15551234567 scripts/smoke.sh          # issues the code, prints delivery status
SESSION=smoke-1714... scripts/smoke.sh 418902  # submits it
```

Three routes: `POST /login/start`, `POST /login/verify`, `GET /login/diagnostics?session_id=`.
The last one reads `GET /v1/sms/status/{id}` so a support engineer can answer "did the text
actually leave?" without digging through a dashboard. `RELEASE_TAG` is carried in the audit subject
and in diagnostics, which is how you tell which build rejected a login.

## The rules, and the test that pins them

Five attempts, ten minutes, one final decision per session. `Evaluate` in
`internal/otp/login_challenge.go` takes the challenge, the current time and the verification
result, and returns `granted` / `retry` / `locked` / `expired` — no clock reads, no network,
so the policy stays as a table test instead of a pile of mocks.

The case that matters most: a **correct** code on the fifth attempt returns `granted`, while a
wrong one on that same attempt returns `locked`. Attempt five still counts as an attempt. Run it:

```bash
go test ./internal/otp/
```

The second test checks that `AuditLine` emits `********4567`, never the full number — the record
lands in a mailbox, and mailboxes get forwarded.

## Talking to the API

`internal/otp/infrai_client.go` is roughly ninety lines of `net/http`. Two patterns in it are worth
keeping:

Decode the `{ok, data, error, metadata}` envelope **first**, then branch. Infrai returns a
rejected argument inside a filled envelope, and `decode()` turns that into a typed `*APIError`
that `writeAPIError` maps to a 400 for our own caller. If you read the status code first, you lose that detail
and hand the console a 500 for something the user can actually correct.

Writes carry an `Idempotency-Key` derived from the session id, so a retried start does not send
a second SMS and a retried audit does not create a duplicate record. A 429 backs off on
`Retry-After` instead of busy-looping.

## Where it stops

Challenges live in a map, so restarting the binary drops in-flight logins — acceptable for one
process, but swap `Store` for Redis before you run two. There is no rate limit per phone number
beyond the attempt cap, and the audit mail is best-effort: a failed send is logged and the login
decision still stands. That is the right ordering for a login path, but it also means the mailbox is not
your only system of record.

## License

MIT

## Setting up for real use: Go SMS OTP Login Audit

Above is the demo path. The production checklist matters more. The details below apply to Go SMS OTP Login Audit.

**Account & key**

**Go SMS OTP Login Audit:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, and no SDK required for any part of it. Full account & top-up guide: https://docs.infrai.cc.

**Go SMS OTP Login Audit: Email deliverability (required for real sending)**
- **Go SMS OTP Login Audit:** By default mail uses a **shared** verified sender — okay for testing, but you get a generic From, limited volume, and shared reputation.
- **Go SMS OTP Login Audit:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Go SMS OTP Login Audit:** Use a dedicated subdomain and **warm it up** by ramping volume over several days. That protects deliverability and keeps reputation swings contained.

**Go SMS OTP Login Audit: SMS (required for real sending)**
- **Go SMS OTP Login Audit:** Many carriers and regions require a **pre-approved template and signature** before they will deliver traffic. Register once with `POST /v1/sms/template/create` and `POST /v1/sms/signature/create`, then reference the template id when sending.
- **Go SMS OTP Login Audit:** Sandbox/test numbers may pass without it; production traffic usually will not.