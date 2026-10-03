"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { StatusBadge } from "@/components/StatusBadge";
import { Alert, EmptyState, LoadingState, PageHeader, PageShell } from "@/components/ui";
import { ApiError, listBookings, type Booking } from "@/lib/api";
import { useAuth, useRequireAuth } from "@/lib/auth";
import { formatBaht, formatDateTime } from "@/lib/format";

type LoadState =
  | { kind: "loading" }
  | { kind: "ok"; items: Booking[] }
  | { kind: "error"; message: string };

export default function BookingsPage() {
  const auth = useRequireAuth();
  const { logout } = useAuth();
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [reloadTick, setReloadTick] = useState(0);
  const authenticated = auth.status === "authenticated";

  useEffect(() => {
    if (!authenticated) return;
    let cancelled = false;
    (async () => {
      try {
        const items = await listBookings();
        if (!cancelled) setState({ kind: "ok", items });
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
  }, [authenticated, reloadTick, logout]);

  return (
    <PageShell className="max-w-6xl space-y-6">
      <PageHeader title="My bookings" description="Track holds, payments, and ticket status for each reservation." />

      {(auth.status !== "authenticated" || state.kind === "loading") && <LoadingState message="Loading your bookings…" />}
      {state.kind === "error" && (
        <Alert variant="error" title="Could not load your bookings" onRetry={() => setReloadTick((n) => n + 1)}>
          {state.message}
        </Alert>
      )}
      {authenticated && state.kind === "ok" && state.items.length === 0 && (
        <EmptyState
          title="No bookings yet"
          description="When you reserve seats, they will show up here with status and payment details."
          action={
            <Link href="/events" className="text-sm font-medium text-slate-900 underline underline-offset-2">
              Browse events
            </Link>
          }
        />
      )}
      {authenticated && state.kind === "ok" && state.items.length > 0 && (
        <ul className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
          {state.items.map((b) => (
            <li key={b.id}>
              <Link
                href={`/bookings/${b.id}`}
                className="flex h-full flex-col rounded-xl border border-slate-200 bg-white p-4 shadow-sm transition-shadow hover:shadow-md"
              >
                <div className="mb-3 flex items-start justify-between gap-2">
                  <span className="line-clamp-2 font-semibold text-slate-900">{b.event_title}</span>
                  <StatusBadge status={b.status} />
                </div>
                <p className="mt-auto text-sm text-slate-600">{formatDateTime(b.starts_at)}</p>
                <p className="mt-1 text-sm text-slate-500">
                  {b.items.map((i) => `${i.row_label}${i.seat_number}`).join(", ")}
                </p>
                <p className="mt-2 text-sm font-medium tabular-nums text-slate-900">{formatBaht(b.total_satang)}</p>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </PageShell>
  );
}
