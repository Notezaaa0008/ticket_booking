"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { StatusBadge } from "@/components/StatusBadge";
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
    <main className="mx-auto max-w-3xl space-y-4 p-6">
      <h1 className="text-xl font-bold">My bookings</h1>
      {(auth.status !== "authenticated" || state.kind === "loading") && <p className="text-gray-500">Loading…</p>}
      {state.kind === "error" && (
        <div className="space-y-2 rounded border border-red-300 bg-red-50 p-4">
          <p className="font-medium text-red-700">Could not load your bookings</p>
          <p className="text-sm text-red-600">{state.message}</p>
          <button type="button" className="text-sm underline" onClick={() => setReloadTick((n) => n + 1)}>
            Retry
          </button>
        </div>
      )}
      {authenticated && state.kind === "ok" && state.items.length === 0 && (
        <p className="text-gray-600">
          You have no bookings yet.{" "}
          <Link href="/events" className="underline">
            Browse events
          </Link>
        </p>
      )}
      {authenticated && state.kind === "ok" && state.items.length > 0 && (
        <ul className="space-y-3">
          {state.items.map((b) => (
            <li key={b.id}>
              <Link href={`/bookings/${b.id}`} className="block space-y-1 rounded border p-4 hover:bg-gray-50">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">{b.event_title}</span>
                  <StatusBadge status={b.status} />
                </div>
                <p className="text-sm text-gray-600">
                  {formatDateTime(b.starts_at)} · {b.items.map((i) => `${i.row_label}${i.seat_number}`).join(", ")} ·{" "}
                  {formatBaht(b.total_satang)}
                </p>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
