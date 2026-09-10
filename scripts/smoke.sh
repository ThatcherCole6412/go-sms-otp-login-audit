#!/usr/bin/env bash
# Drive one login end to end against a locally running otpd.
# Usage: PHONE=+15551234567 scripts/smoke.sh 123456
set -euo pipefail

HOST=${HOST:-http://localhost:8080}
PHONE=${PHONE:?set PHONE to the number that receives the code}
SESSION=${SESSION:-smoke-$(date +%s)}
CODE=${1:-}

curl -sS -X POST "$HOST/login/start" \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\":\"$SESSION\",\"phone\":\"$PHONE\"}"
echo

curl -sS "$HOST/login/diagnostics?session_id=$SESSION"
echo

if [ -z "$CODE" ]; then
  echo "re-run with the code you received: SESSION=$SESSION scripts/smoke.sh 123456"
  exit 0
fi

curl -sS -o /dev/stdout -w '\nHTTP %{http_code}\n' -X POST "$HOST/login/verify" \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\":\"$SESSION\",\"code\":\"$CODE\"}"
