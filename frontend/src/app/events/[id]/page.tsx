"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { ApiError, getEvent, type EventDetail } from "@/lib/api";
import { formatDateTime } from "@/lib/format";

type LoadState =
  | { kind: "loading" }
  | { kind: "ok"; data: EventDetail }
  | { kind: "not_found" }
  | { kind: "error"; message: string };

function EventDetailLoader({ id }: { id: string }) {
  const [reload, setReload] = useState(0);
  const [state, setState] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await getEvent(id);
        if (!cancelled) setState({ kind: "ok", data });
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
  }, [id, reload]);

  if (state.kind === "loading") {
    return <p className="text-gray-500">Loading event…</p>;
  }

  if (state.kind === "not_found") {
    return <p className="text-gray-600">This event was not found or is no longer available.</p>;
  }

  if (state.kind === "error") {
    return (
      <div className="rounded border border-red-300 bg-red-50 p-4 space-y-2">
        <p className="font-medium text-red-700">Could not load event</p>
        <p className="text-sm text-red-600">{state.message}</p>
        <button type="button" onClick={() => setReload((n) => n + 1)} className="text-sm underline">
          Retry
        </button>
      </div>
    );
  }

  return (
    <>
      <header className="space-y-2">
        <h1 className="text-2xl font-bold">{state.data.title}</h1>
        <p className="text-gray-600">{state.data.venue}</p>
        {state.data.description && <p className="text-sm">{state.data.description}</p>}
      </header>

      <section className="space-y-3">
        <h2 className="font-semibold">Upcoming showtimes</h2>
        {state.data.showtimes.length === 0 ? (
          <p className="text-gray-600">No upcoming showtimes on sale.</p>
        ) : (
          <ul className="space-y-2">
            {state.data.showtimes.map((st) => (
              <li key={st.id}>
                <Link
                  href={`/showtimes/${st.id}`}
                  className="flex items-center justify-between rounded border px-4 py-3 hover:bg-gray-50"
                >
                  <span>{formatDateTime(st.starts_at)}</span>
                  <span className="text-sm text-gray-600">{st.available_seat_count} seats available</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </section>
    </>
  );
}

export default function EventDetailPage() {
  const params = useParams();
  const id = typeof params.id === "string" ? params.id : "";

  return (
    <main className="mx-auto max-w-2xl p-6 space-y-6">
      <Link href="/events" className="text-sm text-gray-600 underline">
        ← All events
      </Link>

      {!id ? (
        <p className="text-gray-600">Missing event id.</p>
      ) : (
        <EventDetailLoader key={id} id={id} />
      )}
    </main>
  );
}
