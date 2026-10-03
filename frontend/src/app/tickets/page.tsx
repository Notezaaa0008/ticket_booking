"use client";

import { QRCodeSVG } from "qrcode.react";
import { useEffect, useState } from "react";
import { ApiError, listTickets, type Ticket, type TicketStatus } from "@/lib/api";
import { useAuth, useRequireAuth } from "@/lib/auth";
import { formatDateTime } from "@/lib/format";

type LoadState = { kind: "loading" } | { kind: "ok"; tickets: Ticket[] } | { kind: "error"; message: string };

const STATUS_LABEL: Record<TicketStatus, string> = {
  VALID: "Valid",
  USED: "Used",
  VOID: "Void",
};

export default function TicketsPage() {
  const auth = useRequireAuth();
  const { logout } = useAuth();
  const authenticated = auth.status === "authenticated";
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (!authenticated) return;
    let cancelled = false;
    (async () => {
      try {
        const tickets = await listTickets();
        if (!cancelled) setState({ kind: "ok", tickets });
      } catch (e) {
        if (cancelled) return;
        if (e instanceof ApiError && e.status === 401) {
          logout();
          return;
        }
        setState({ kind: "error", message: e instanceof Error ? e.message : "Unknown error" });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [authenticated, tick, logout]);

  return (
    <main className="mx-auto max-w-2xl space-y-4 p-6">
      <h1 className="text-xl font-bold">My tickets</h1>
      {(!authenticated || state.kind === "loading") && <p className="text-gray-500">Loading tickets…</p>}
      {authenticated && state.kind === "error" && (
        <div className="space-y-2 rounded border border-red-300 bg-red-50 p-4">
          <p className="font-medium text-red-700">Could not load your tickets</p>
          <p className="text-sm text-red-600">{state.message}</p>
          <button type="button" className="text-sm underline" onClick={() => setTick((n) => n + 1)}>
            Retry
          </button>
        </div>
      )}
      {authenticated && state.kind === "ok" && state.tickets.length === 0 && (
        <p className="text-gray-600">You have no tickets yet. Tickets appear here after a successful payment.</p>
      )}
      {authenticated && state.kind === "ok" && state.tickets.length > 0 && (
        <ul className="space-y-3">
          {state.tickets.map((t) => (
            <li key={t.code} className="flex gap-4 rounded border p-4">
              <QRCodeSVG value={t.code} size={112} title={`Ticket ${t.seat_label}`} />
              <div className="space-y-1 text-sm">
                <p className="font-medium">{t.event_title}</p>
                <p className="text-gray-600">
                  {t.venue} · {formatDateTime(t.starts_at)}
                </p>
                <p>
                  Seat {t.seat_label} ({t.zone})
                </p>
                <p>Status: {STATUS_LABEL[t.status]}</p>
                <p className="font-mono text-xs text-gray-500">{t.code}</p>
              </div>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
