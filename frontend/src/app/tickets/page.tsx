"use client";

import { QRCodeSVG } from "qrcode.react";
import { useEffect, useState } from "react";
import { TicketQrModal } from "@/components/TicketQrModal";
import { Alert, Button, EmptyState, LoadingState, PageHeader, PageShell } from "@/components/ui";
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
  const [expanded, setExpanded] = useState<Ticket | null>(null);

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
    <PageShell className="max-w-6xl space-y-6">
      <PageHeader title="My tickets" description="Show these QR codes at the venue entrance." />

      {(!authenticated || state.kind === "loading") && <LoadingState message="Loading tickets…" />}
      {authenticated && state.kind === "error" && (
        <Alert variant="error" title="Could not load your tickets" onRetry={() => setTick((n) => n + 1)}>
          {state.message}
        </Alert>
      )}
      {authenticated && state.kind === "ok" && state.tickets.length === 0 && (
        <EmptyState
          title="No tickets yet"
          description="Tickets appear here after you complete payment for a booking."
        />
      )}
      {authenticated && state.kind === "ok" && state.tickets.length > 0 && (
        <ul className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
          {state.tickets.map((t) => (
            <li key={t.code}>
              <TicketCard ticket={t} onEnlarge={() => setExpanded(t)} />
            </li>
          ))}
        </ul>
      )}
      {expanded && <TicketQrModal ticket={expanded} onClose={() => setExpanded(null)} />}
    </PageShell>
  );
}

function TicketCard({ ticket, onEnlarge }: { ticket: Ticket; onEnlarge: () => void }) {
  return (
    <article className="flex h-full flex-col rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <button
        type="button"
        className="group mx-auto rounded-lg border border-slate-100 bg-white p-2 transition hover:border-slate-300 hover:shadow-sm focus:outline-none focus:ring-2 focus:ring-slate-300"
        onClick={onEnlarge}
        aria-label={`Enlarge QR code for seat ${ticket.seat_label}`}
      >
        <QRCodeSVG value={ticket.code} size={120} title={`Ticket ${ticket.seat_label}`} />
        <span className="mt-2 block text-xs font-medium text-slate-500 group-hover:text-slate-800">Tap to enlarge</span>
      </button>
      <div className="mt-4 flex flex-1 flex-col space-y-1 text-sm">
        <p className="line-clamp-2 font-semibold text-slate-900">{ticket.event_title}</p>
        <p className="text-slate-500">
          {ticket.venue} · {formatDateTime(ticket.starts_at)}
        </p>
        <p className="text-slate-700">
          Seat {ticket.seat_label} · {ticket.zone}
        </p>
        <p className="text-slate-600">Status: {STATUS_LABEL[ticket.status]}</p>
        <p className="truncate font-mono text-xs text-slate-400" title={ticket.code}>
          {ticket.code}
        </p>
      </div>
      <Button variant="secondary" className="mt-4 w-full text-xs" onClick={onEnlarge}>
        Enlarge QR
      </Button>
    </article>
  );
}
