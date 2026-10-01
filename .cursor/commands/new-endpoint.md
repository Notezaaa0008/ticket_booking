# /new-endpoint

Add a new API endpoint following the project conventions. I will give the method, path,
and purpose after this command.

Process:
1. Check docs/plan.md: is this endpoint in the current phase? If not, stop and tell me.
2. Write a short plan: request/response JSON, status codes, auth requirement, DB tables touched.
   If it touches anything listed in .cursorrules Section 10, STOP and wait for my approval.
3. Implement in layers: repository -> service -> handler -> route registration.
   Keep the handler thin. Use the standard error shape. Validate all input.
4. Write tests: at least one success case, one validation failure, one auth failure
   (if protected), and one edge case relevant to the logic.
5. Update docs/api.md with the method, path, auth, request, response, and error codes.
6. Run: cd backend && go vet ./... && go test ./... -race -count=1 and paste the real output.
7. Finish with the standard final report format from .cursorrules Section 12.