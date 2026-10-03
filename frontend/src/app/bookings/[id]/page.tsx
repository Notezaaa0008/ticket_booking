"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { StatusBadge } from "@/components/StatusBadge";
import { ApiError, cancelBooking, getBooking, type Booking } from "@/lib/api";
import { useAuth, useRequireAuth } from "@/lib/auth";
import { formatBaht, formatDateTime } from "@/lib/format";

type LoadState =
  | { kind: "loading" }
  | { kind: "ok"; booking: Booking }
  | { kind: "not_found" }
  | { kind: "error"; message: string };

const POLL_MS = 5000;

function formatCountdown(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

function StatusPanel({ booking, remainingMs }: { booking: Booking; remainingMs: number }) {
  switch (booking.status) {
    case "PENDING":
      return remainingMs > 0 ? (
        <div className="rounded border border-yellow-400 bg-yellow-50 p-4">
          <p className="font-medium text-yellow-900">Seats are held for you</p>
          <p className="text-2xl font-bold" aria-live="polite">
            {formatCountdown(remainingMs)}
          </p>
          <p className="text-sm text-yellow-800">Payment is not available yet; the hold ends when the timer reaches zero.</p>
        </div>
      ) : (
        <div className="rounded border border-yellow-400 bg-yellow-50 p-4">
          <p className="font-medium text-yellow-900">The hold time has ended</p>
          <p className="text-sm text-yellow-800">Waiting for the system to release your seats. This page updates automatically.</p>
        </div>
      );
    case "EXPIRED":
      return (
        <div className="rounded border border-gray-400 bg-gray-50 p-4">
          <p className="font-medium">Booking expired</p>
          <p className="text-sm text-gray-700">
            {booking.status_reason === "HOLD_EXPIRED"
              ? "The hold time ran out before the booking was paid. The seats were released."
              : "This booking expired and the seats were released."}
          </p>
        </div>
      );
    case "CANCELLED":
      return (
        <div className="rounded border border-red-300 bg-red-50 p-4">
          <p className="font-medium text-red-800">Booking cancelled</p>
          <p className="text-sm text-red-700">
            {booking.status_reason === "USER_CANCELLED"
              ? "You cancelled this booking. The seats were released."
              : "This booking was cancelled and the seats were released."}
          </p>
        </div>
      );
    case "PAID":
      return (
        <div className="rounded border border-green-400 bg-green-50 p-4">
          <p className="font-medium text-green-800">Booking paid</p>
          <p className="text-sm text-green-700">Your payment was received.</p>
        </div>
      );
    case "REFUNDED":
      return (
        <div className="rounded border border-blue-400 bg-blue-50 p-4">
          <p className="font-medium text-blue-800">Booking refunded</p>
          <p className="text-sm text-blue-700">This booking was refunded.</p>
        </div>
      );
  }
}

export default function BookingDetailPage() {
  const params = useParams();
  const id = typeof params.id === "string" ? params.id : "";
  const auth = useRequireAuth();
  const { logout } = useAuth();
  const authenticated = auth.status === "authenticated";

  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [tick, setTick] = useState(0);
  const [now, setNow] = useState(() => Date.now());
  const [cancelling, setCancelling] = useState(false);
  const [cancelError, setCancelError] = useState<string | null>(null);

  const status = state.kind === "ok" ? state.booking.status : null;

  useEffect(() => {
    if (!authenticated || !id) return;
    let cancelled = false;
    (async () => {
      try {
        const booking = await getBooking(id);
        if (!cancelled) setState({ kind: "ok", booking });
      } catch (e) {
        if (cancelled) return;
        if (e instanceof ApiError && e.status === 401) {
          logout();
          return;
        }
        if (e instanceof ApiError && e.status === 404) {
          setState({ kind: "not_found" });
          return;
        }
        setState({ kind: "error", message: e instanceof Error ? e.message : "Unknown error" });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [authenticated, id, tick, logout]);

  // While PENDING: refetch every 5s (status is decided by the server) and tick the clock every second.
  useEffect(() => {
    if (status !== "PENDING") return;
    const poll = window.setInterval(() => setTick((n) => n + 1), POLL_MS);
    const clock = window.setInterval(() => setNow(Date.now()), 1000);
    return () => {
      window.clearInterval(poll);
      window.clearInterval(clock);
    };
  }, [status]);

  const onCancel = async () => {
    if (!window.confirm("Cancel this booking and release the seats?")) return;
    setCancelling(true);
    setCancelError(null);
    try {
      const booking = await cancelBooking(id);
      setState({ kind: "ok", booking });
    } catch (e) {
      if (e instanceof ApiError && e.code === "BOOKING_NOT_PENDING") {
        setCancelError("This booking can no longer be cancelled because it is not pending anymore.");
        setTick((n) => n + 1);
      } else if (e instanceof ApiError && e.code === "BOOKING_NOT_FOUND") {
        setCancelError("This booking was not found.");
      } else {
        setCancelError(e instanceof Error ? e.message : "Could not cancel the booking.");
      }
    } finally {
      setCancelling(false);
    }
  };

  return (
    <main className="mx-auto max-w-2xl space-y-4 p-6">
      <Link href="/bookings" className="text-sm text-gray-600 underline">
        ← My bookings
      </Link>
      {(!authenticated || state.kind === "loading") && <p className="text-gray-500">Loading booking…</p>}
      {authenticated && state.kind === "not_found" && <p className="text-gray-600">Booking not found.</p>}
      {authenticated && state.kind === "error" && (
        <div className="space-y-2 rounded border border-red-300 bg-red-50 p-4">
          <p className="font-medium text-red-700">Could not load the booking</p>
          <p className="text-sm text-red-600">{state.message}</p>
          <button type="button" className="text-sm underline" onClick={() => setTick((n) => n + 1)}>
            Retry
          </button>
        </div>
      )}
      {authenticated && state.kind === "ok" && (
        <>
          <div className="flex items-center justify-between gap-2">
            <h1 className="text-xl font-bold">{state.booking.event_title}</h1>
            <StatusBadge status={state.booking.status} />
          </div>
          <p className="text-sm text-gray-600">
            {state.booking.venue} · {formatDateTime(state.booking.starts_at)}
          </p>
          <StatusPanel booking={state.booking} remainingMs={new Date(state.booking.expires_at).getTime() - now} />
          <ul className="divide-y rounded border">
            {state.booking.items.map((it) => (
              <li key={it.seat_id} className="flex justify-between p-3 text-sm">
                <span>
                  Seat {it.row_label}
                  {it.seat_number} ({it.zone})
                </span>
                <span>{formatBaht(it.price_satang)}</span>
              </li>
            ))}
            <li className="flex justify-between p-3 font-medium">
              <span>Total</span>
              <span>{formatBaht(state.booking.total_satang)}</span>
            </li>
          </ul>
          {cancelError && (
            <p role="alert" className="rounded border border-red-300 bg-red-50 p-2 text-sm text-red-700">
              {cancelError}
            </p>
          )}
          {state.booking.status === "PENDING" && (
            <button
              type="button"
              disabled={cancelling}
              onClick={onCancel}
              className="rounded border border-red-500 px-4 py-2 text-sm text-red-700 disabled:opacity-50"
            >
              {cancelling ? "Cancelling…" : "Cancel booking"}
            </button>
          )}
        </>
      )}
    </main>
  );
}
