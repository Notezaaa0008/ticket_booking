#!/usr/bin/env bash
# Runs every standard check and prints a short summary. Run it yourself; paste only failures into Cursor.
# Use NO_RACE=1 ./scripts/check.sh if the Go race detector is not available on your machine.
cd "$(dirname "$0")/.." || exit 1
failed=()

run() {
  local name="$1"; shift
  echo "=== $name"
  if "$@"; then echo "OK: $name"; else echo "FAILED: $name"; failed+=("$name"); fi
}

run "go vet"  bash -c 'cd backend && go vet ./...'
run "gofmt"   bash -c 'cd backend && out=$(gofmt -l .) && [ -z "$out" ] || { echo "$out"; exit 1; }'
run "go test" bash -c 'cd backend && if [ -n "$NO_RACE" ]; then go test ./... -count=1; else go test ./... -race -count=1; fi'
if [ -f frontend/package.json ]; then
  run "eslint"     bash -c 'cd frontend && npm run lint'
  run "tsc"        bash -c 'cd frontend && npx tsc --noEmit'
  run "next build" bash -c 'cd frontend && npm run build'
fi

echo
if [ ${#failed[@]} -eq 0 ]; then echo "ALL CHECKS PASSED"; else echo "FAILED: ${failed[*]}"; exit 1; fi
