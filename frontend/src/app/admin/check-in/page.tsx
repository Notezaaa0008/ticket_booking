"use client";

import { useState, type FormEvent } from "react";
import { AdminGuard, errorText } from "@/components/AdminGuard";
import { Alert, Button, Field, Input, PageHeader, Panel } from "@/components/ui";
import { adminCheckIn, ApiError, type Ticket } from "@/lib/api";
import { formatDateTime } from "@/lib/format";

type Result =
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "success"; ticket: Ticket }
  | { kind: "used" }
  | { kind: "void" }
  | { kind: "not_found" }
  | { kind: "error"; message: string };

export default function AdminCheckInPage() {
  return (
    <AdminGuard>
      <CheckIn />
    </AdminGuard>
  );
}

function CheckIn() {
  const [code, setCode] = useState("");
  const [result, setResult] = useState<Result>({ kind: "idle" });

  async function submit(e: FormEvent) {
    e.preventDefault();
    const c = code.trim();
    if (!c) return;
    setResult({ kind: "checking" });
    try {
      const res = await adminCheckIn(c);
      setResult({ kind: "success", ticket: res.ticket });
    } catch (err) {
      if (err instanceof ApiError && err.code === "TICKET_ALREADY_USED") setResult({ kind: "used" });
      else if (err instanceof ApiError && err.code === "TICKET_VOID") setResult({ kind: "void" });
      else if (err instanceof ApiError && err.code === "NOT_FOUND") setResult({ kind: "not_found" });
      else setResult({ kind: "error", message: errorText(err) });
    }
  }

  return (
    <div className="mx-auto max-w-lg space-y-6">
      <PageHeader title="Ticket check-in" description="Scan or enter a ticket code to mark entry at the venue." />

      <Panel>
        <form onSubmit={submit} className="flex flex-col gap-4 sm:flex-row sm:items-end">
          <Field label="Ticket code" className="flex-1">
            <Input
              autoFocus
              className="font-mono"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="Paste ticket code"
            />
          </Field>
          <Button type="submit" disabled={result.kind === "checking"} className="sm:mb-0.5">
            {result.kind === "checking" ? "Checking…" : "Check in"}
          </Button>
        </form>
      </Panel>

      {result.kind === "success" && (
        <Alert variant="success" title="Checked in">
          {result.ticket.seat_label} · {result.ticket.event_title} at {result.ticket.venue} ·{" "}
          {formatDateTime(result.ticket.starts_at)}
        </Alert>
      )}
      {result.kind === "used" && (
        <Alert variant="warning" title="Already used">
          This ticket was checked in before. Each ticket can only be used once.
        </Alert>
      )}
      {result.kind === "void" && (
        <Alert variant="error" title="Ticket void">
          This ticket is void (refunded or its booking is not paid).
        </Alert>
      )}
      {result.kind === "not_found" && (
        <Alert variant="error" title="Not found">
          No ticket matches that code. Check for typos and try again.
        </Alert>
      )}
      {result.kind === "error" && <Alert variant="error">{result.message}</Alert>}
    </div>
  );
}
