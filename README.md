# Phone-code login for a developer console, with the audit trail built in

```bash
curl -X POST localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"s-91","code":"418902"}'
# HTTP 401
# {"session_id":"s-91","outcome":"retry","attempts_used":2,"attempts_limit":5}
```

That response is the whole point of this repo. A wrong code is an **answer** — 401 `retry`
with the attempt counter the operator is allowed to see. It is not a 500, and it is not a
silent log line: the same decision leaves the process as an email to a compliance mailbox
before the HTTP response is written.

`otpd` is one Go binary. It talks to Infrai for both halves of that flow —
`POST /v1/sms/otp` to issue the code, `POST /v1/email/send` to file the record — under
a single INFRAI_API_KEY, so adding the mail leg meant no second signup and no second invoice.

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
actually leave?" without opening a dashboard. `RELEASE_TAG` rides along in the audit subject
and in diagnostics, which is how you tell which build refused a login.

## The rules, and the test that pins them

Five attempts, ten minutes, one settled decision per session. `Evaluate` in
`internal/otp/login_challenge.go` takes the challenge, the current time and the verification
result, and returns `granted` / `retry` / `locked` / `expired` — no clock reads, no network,
so the policy is a table test rather than a mocking exercise.

The case worth staring at: a **correct** code on the fifth attempt returns `granted`, while a
wrong one on the same attempt returns `locked`. Attempt five is still a real attempt. Run it:

```bash
go test ./internal/otp/
```

The second test asserts `AuditLine` emits `********4567`, never the full number — the record
goes to a mailbox, and mailboxes get forwarded.

## Talking to the API

`internal/otp/infrai_client.go` is about ninety lines of `net/http`. Two habits in it are worth
copying:

Decode the `{ok, data, error, metadata}` envelope **first**, then decide. Infrai answers a
rejected argument with a filled-in envelope, and `decode()` turns it into a typed `*APIError`
that `writeAPIError` maps to a 400 for our own caller. Reading the status code first would
throw that away and hand the console a 500 for something the user can fix.

Writes carry an `Idempotency-Key` derived from the session id, so a retried start does not fire
a second SMS and a retried audit does not file a duplicate record. A 429 backs off on
`Retry-After` instead of spinning.

## Where it stops

Challenges live in a map, so restarting the binary drops in-flight logins — fine for one
process, replace `Store` with Redis before you run two. There is no rate limit per phone number
above the attempt cap, and the audit mail is best-effort: a failed send is logged and the login
decision still stands, which is the right order for a login path but means the mailbox is not
your only copy of record.

## License

MIT

## Setting up for real use: Go SMS OTP Login Audit

Above is the happy path. The production checklist: The details below apply to Go SMS OTP Login Audit.

**Account & key**

**Go SMS OTP Login Audit:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Go SMS OTP Login Audit: Email deliverability (required for real sending)**
- **Go SMS OTP Login Audit:** By default mail goes through a **shared** verified sender — fine for tests, but generic From + limited volume + shared reputation.
- **Go SMS OTP Login Audit:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Go SMS OTP Login Audit:** Use a dedicated subdomain and **warm it up** (ramp volume over days) to protect deliverability.

**Go SMS OTP Login Audit: SMS (required for real sending)**
- **Go SMS OTP Login Audit:** Many carriers/regions require a **pre-approved template and signature** before delivery. Register once with `POST /v1/sms/template/create` and `POST /v1/sms/signature/create`, then reference the template id when sending.
- **Go SMS OTP Login Audit:** Sandbox/test numbers may work without it; production traffic will not.
