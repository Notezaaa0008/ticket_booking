#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
failed=()

# --- integration env (override ได้จากภายนอก / CI) ---
export TEST_DATABASE_URL=${TEST_DATABASE_URL:-"postgres://ticket:ticket@localhost:5432/ticket_booking_test?sslmode=disable"}
export REDIS_URL="${REDIS_URL:-redis://localhost:6379/1}"
export REQUIRE_INTEGRATION="${REQUIRE_INTEGRATION:-1}"
export NO_RACE="${NO_RACE:-}"

# ปิด integration ชั่วคราว: SKIP_INTEGRATION=1 scripts/check.sh
if [ -n "${SKIP_INTEGRATION:-}" ]; then
  unset TEST_DATABASE_URL REDIS_URL
  export REQUIRE_INTEGRATION=0
  echo "WARNING: integration tests disabled (SKIP_INTEGRATION=1)"
fi

run() {
  local name="$1"; shift
  echo "=== $name"
  if "$@"; then echo "OK: $name"; else echo "FAILED: $name"; failed+=("$name"); fi
}

# --- preflight: ถ้าจะบังคับ integration ต้องต่อ DB/Redis ได้จริง ---
preflight() {
  [ "$REQUIRE_INTEGRATION" = "1" ] || return 0
  local ok=0
  if command -v pg_isready >/dev/null 2>&1; then
    pg_isready -d "$TEST_DATABASE_URL" >/dev/null 2>&1 || { echo "postgres not reachable: $TEST_DATABASE_URL"; ok=1; }
  else
    docker compose exec -T postgres pg_isready >/dev/null 2>&1 || { echo "postgres not reachable (docker compose)"; ok=1; }
  fi
  docker compose exec -T redis redis-cli PING >/dev/null 2>&1 || { echo "redis not reachable: $REDIS_URL"; ok=1; }
  [ $ok -eq 0 ] || echo "hint: docker compose up -d postgres redis  (หรือ SKIP_INTEGRATION=1 เพื่อข้าม)"
  return $ok
}
run "preflight (postgres/redis)" preflight

run "go vet"  bash -c 'cd backend && go vet ./...'
run "gofmt"   bash -c 'cd backend && out=$(gofmt -l .) && [ -z "$out" ] || { echo "$out"; exit 1; }'

# -v + เก็บ log ไว้ตรวจว่า TestCatalog* ไม่ได้ถูก skip
run "go test" bash -c '
  cd backend
  if [ -n "$NO_RACE" ]; then
    go test ./... -count=1 -v 2>&1 | tee /tmp/gotest.log
  else
    go test ./... -race -count=1 -v 2>&1 | tee /tmp/gotest.log
  fi
  exit "${PIPESTATUS[0]}"'

run "catalog tests not skipped" bash -c '
  [ "$REQUIRE_INTEGRATION" = "1" ] || exit 0
  if grep -qE "^(=== SKIP|--- SKIP).*TestCatalog" /tmp/gotest.log; then
    echo "catalog integration tests were SKIPPED:"; grep -E "SKIP.*TestCatalog" /tmp/gotest.log; exit 1
  fi
  if ! grep -qE "^--- PASS: TestCatalog" /tmp/gotest.log; then
    echo "no TestCatalog* ran at all"; exit 1
  fi
  echo "catalog integration tests executed:"; grep -cE "^--- PASS: TestCatalog" /tmp/gotest.log'

if [ -f frontend/package.json ]; then
  run "eslint"     bash -c 'cd frontend && npm run lint'
  run "tsc"        bash -c 'cd frontend && npx tsc --noEmit'
  run "next build" bash -c 'cd frontend && npm run build'
fi

echo
if [ ${#failed[@]} -eq 0 ]; then echo "ALL CHECKS PASSED"; else echo "FAILED: ${failed[*]}"; exit 1; fi