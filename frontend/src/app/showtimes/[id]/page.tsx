"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { ApiError, createBooking, getShowtimeSeats, type SeatItem, type ShowtimeSeatsResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatBaht } from "@/lib/format";

type LoadState =
  | { kind: "loading" }
  | { kind: "ok"; data: ShowtimeSeatsResponse }
  | { kind: "not_found" }
  | { kind: "error"; message: string };

type SeatVisualStatus = SeatItem["status"] | "selected";

function seatLabel(seat: SeatItem | undefined, fallback: string): string {
  return seat ? `${seat.row_label}${seat.seat_number}` : fallback;
}

function seatClass(status: SeatVisualStatus): string {
  const base = "h-8 w-8 rounded text-xs font-medium ";
  switch (status) {
    case "available":
      return base + "bg-green-100 border border-green-400 hover:bg-green-200 cursor-pointer";
    case "held":
      return base + "bg-yellow-100 border border-yellow-500 cursor-not-allowed opacity-80";
    case "sold":
      return base + "bg-gray-200 border border-gray-400 cursor-not-allowed opacity-60";
    case "selected":
      return base + "bg-blue-600 text-white border border-blue-800 cursor-pointer";
  }
}

function bookingErrorMessage(e: unknown, seats: SeatItem[]): string {
  if (!(e instanceof ApiError)) return "Could not reach the server. Please try again.";
  switch (e.code) {
    case "SEAT_UNAVAILABLE": {
      const byId = new Map(seats.map((s) => [s.id, s]));
      const names = e.seatIds.map((id) => seatLabel(byId.get(id), "a seat"));
      return `Sorry, ${names.length > 0 ? `seat ${names.join(", ")} ${names.length > 1 ? "were" : "was"}` : "some seats were"} just taken. The seat map was refreshed; your other selections are kept.`;
    }
    case "TOO_MANY_SEATS":
      return "You can book at most 6 seats at a time.";
    case "SHOWTIME_NOT_ON_SALE":
      return "This showtime is no longer on sale.";
    case "RATE_LIMITED":
      return "Too many booking attempts. Please wait a minute and try again.";
    case "VALIDATION_FAILED":
      return e.message;
    case "UNAUTHENTICATED":
      return "Your session expired. Please log in again.";
    default:
      return e.message;
  }
}

function ShowtimeSeatsLoader({ showtimeId }: { showtimeId: string }) {
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [pollTick, setPollTick] = useState(0);
  const { state: auth } = useAuth();
  const router = useRouter();
  const [booking, setBooking] = useState(false);
  const [bookError, setBookError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await getShowtimeSeats(showtimeId);
        if (cancelled) return;
        setState({ kind: "ok", data });
        setSelected((prev) => {
          const avail = new Set(data.seats.filter((s) => s.status === "available").map((s) => s.id));
          const next = new Set<string>();
          prev.forEach((sid) => {
            if (avail.has(sid)) next.add(sid);
          });
          return next;
        });
      } catch (e) {
        if (cancelled) return;
        if (e instanceof ApiError && e.status === 404) {
          setState({ kind: "not_found" });
          return;
        }
        setState({
          kind: "error",
          message: e instanceof Error ? e.message : "Unknown error",
        });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [showtimeId, pollTick]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") {
        setPollTick((n) => n + 1);
      }
    }, 10_000);
    return () => window.clearInterval(timer);
  }, []);

  const rows = useMemo(() => {
    if (state.kind !== "ok") return [];
    const map = new Map<string, SeatItem[]>();
    for (const seat of state.data.seats) {
      const list = map.get(seat.row_label) ?? [];
      list.push(seat);
      map.set(seat.row_label, list);
    }
    return [...map.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [state]);

  const totalSatang = useMemo(() => {
    if (state.kind !== "ok") return 0;
    const priceByID = new Map(state.data.seats.map((s) => [s.id, s.price_satang]));
    let sum = 0;
    selected.forEach((sid) => {
      sum += priceByID.get(sid) ?? 0;
    });
    return sum;
  }, [state, selected]);

  const toggleSeat = (seat: SeatItem) => {
    if (seat.status !== "available") return;
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(seat.id)) next.delete(seat.id);
      else next.add(seat.id);
      return next;
    });
  };

  const onBook = async () => {
    if (state.kind !== "ok" || selected.size === 0) return;
    if (auth.status !== "authenticated") {
      router.push(`/login?next=${encodeURIComponent(`/showtimes/${showtimeId}`)}`);
      return;
    }
    setBooking(true);
    setBookError(null);
    try {
      const created = await createBooking(showtimeId, [...selected]);
      router.push(`/bookings/${created.id}`);
    } catch (e) {
      setBookError(bookingErrorMessage(e, state.data.seats));
      if (e instanceof ApiError && e.code === "SEAT_UNAVAILABLE") {
        // Drop only the taken seats from the selection, keep the rest, and refresh the map.
        const taken = new Set(e.seatIds);
        setSelected((prev) => new Set([...prev].filter((sid) => !taken.has(sid))));
        setPollTick((n) => n + 1);
      }
      if (e instanceof ApiError && e.code === "UNAUTHENTICATED") {
        router.push(`/login?next=${encodeURIComponent(`/showtimes/${showtimeId}`)}`);
      }
      setBooking(false);
    }
  };

  if (state.kind === "loading") {
    return <p className="text-gray-500">Loading seat map…</p>;
  }

  if (state.kind === "not_found") {
    return <p className="text-gray-600">Showtime not found.</p>;
  }

  if (state.kind === "error") {
    return (
      <div className="rounded border border-red-300 bg-red-50 p-4 space-y-2">
        <p className="font-medium text-red-700">Could not load seats</p>
        <p className="text-sm text-red-600">{state.message}</p>
        <button type="button" onClick={() => setPollTick((n) => n + 1)} className="text-sm underline">
          Retry
        </button>
      </div>
    );
  }

  return (
    <>
      <div className="flex flex-wrap gap-4 text-sm text-gray-600">
        <span>Available: {state.data.summary.available}</span>
        <span>Held: {state.data.summary.held}</span>
        <span>Sold: {state.data.summary.sold}</span>
      </div>

      {state.data.seats.length === 0 ? (
        <p className="text-gray-600">No seats configured for this showtime.</p>
      ) : (
        <div className="space-y-4 overflow-x-auto">
          {rows.map(([row, seats]) => (
            <div key={row} className="flex items-center gap-2">
              <span className="w-8 font-medium">{row}</span>
              <div className="flex flex-wrap gap-1">
                {seats
                  .sort((a, b) => a.seat_number - b.seat_number)
                  .map((seat) => {
                    const visual: SeatVisualStatus = selected.has(seat.id) ? "selected" : seat.status;
                    return (
                      <button
                        key={seat.id}
                        type="button"
                        title={`${seat.row_label}${seat.seat_number} · ${formatBaht(seat.price_satang)} · ${seat.status}`}
                        className={seatClass(visual)}
                        disabled={seat.status !== "available" && !selected.has(seat.id)}
                        onClick={() => toggleSeat(seat)}
                        aria-label={`Row ${seat.row_label} seat ${seat.seat_number}`}
                      >
                        {seat.seat_number}
                      </button>
                    );
                  })}
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="rounded border p-4 flex flex-wrap items-center justify-between gap-4">
        <p className="font-medium">
          Selected {selected.size} seat{selected.size === 1 ? "" : "s"} · Total {formatBaht(totalSatang)}
        </p>
        <button
          type="button"
          disabled={booking || selected.size === 0 || auth.status === "loading"}
          onClick={onBook}
          className="rounded bg-black px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-50"
        >
          {booking ? "Booking…" : auth.status === "authenticated" ? "Book selected seats" : "Log in to book"}
        </button>
      </div>
      {bookError && (
        <p role="alert" className="rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700">
          {bookError}
        </p>
      )}

      <p className="text-xs text-gray-500">Seat map refreshes every 10 seconds while this tab is open.</p>
    </>
  );
}

export default function ShowtimeSeatsPage() {
  const params = useParams();
  const showtimeId = typeof params.id === "string" ? params.id : "";

  return (
    <main className="mx-auto max-w-3xl p-6 space-y-6">
      <Link href="/events" className="text-sm text-gray-600 underline">
        ← Events
      </Link>

      {!showtimeId ? (
        <p className="text-gray-600">Missing showtime id.</p>
      ) : (
        <ShowtimeSeatsLoader key={showtimeId} showtimeId={showtimeId} />
      )}
    </main>
  );
}
