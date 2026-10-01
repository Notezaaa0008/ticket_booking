# /verify-phase

You claimed this phase is done. Do NOT trust that claim. Prove it.

Steps (do all, in order):
1. Open docs/plan.md and find the checklist for the CURRENT phase. Quote it in full.
2. Run these commands for real and paste the complete output (no summarizing):
   - docker compose ps
   - cd backend && go vet ./... && gofmt -l . && go test ./... -race -count=1
   - cd frontend && npm run lint && npx tsc --noEmit && npm run build
   Skip a command only if that part does not exist yet, and say so explicitly.
3. If the app can run, start it and call the key endpoints with curl (show the request and
   the real response), for example the health check and the endpoints added this phase.
4. For every checklist item, output a table row:
   | # | Criterion | Evidence (file:line or test name or command output) | PASS / FAIL / NOT DONE |
   An item is PASS only if you can point to concrete evidence. "Implemented" is not evidence.
5. List separately: anything mocked, skipped, hard-coded, TODO, or untested.
6. Search the diff for red flags and report what you find: TODO, FIXME, panic("not implemented"),
   hard-coded secrets, float used for money, ignored errors (`_ =`), skipped or deleted tests,
   handlers containing business logic, and any change outside the current phase scope.
7. Finish with one verdict line: "PHASE VERIFIED" only if every item is PASS and all commands
   succeeded; otherwise "PHASE NOT VERIFIED" followed by the exact list of failures and a fix plan.
Do not fix anything in this command unless I ask. Report only.