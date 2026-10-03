"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { ApiError, listEvents, type EventListItem, type EventListResponse } from "@/lib/api";
import { formatBaht, formatDateTime } from "@/lib/format";

type LoadState =
  | { kind: "loading" }
  | { kind: "empty" }
  | { kind: "ok"; data: EventListResponse }
  | { kind: "error"; message: string; code?: string };

function SearchForm({
  initialQ,
  initialFrom,
  initialTo,
  onSearch,
}: {
  initialQ: string;
  initialFrom: string;
  initialTo: string;
  onSearch: (q: string, from: string, to: string) => void;
}) {
  const [formQ, setFormQ] = useState(initialQ);
  const [formFrom, setFormFrom] = useState(initialFrom);
  const [formTo, setFormTo] = useState(initialTo);

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        onSearch(formQ.trim(), formFrom, formTo);
      }}
      className="space-y-3 rounded border p-4"
    >
      <label className="block text-sm">
        Search
        <input
          className="mt-1 w-full rounded border px-3 py-2"
          value={formQ}
          onChange={(e) => setFormQ(e.target.value)}
          placeholder="Title or venue"
        />
      </label>
      <div className="grid grid-cols-2 gap-3">
        <label className="block text-sm">
          From
          <input
            type="date"
            className="mt-1 w-full rounded border px-3 py-2"
            value={formFrom}
            onChange={(e) => setFormFrom(e.target.value)}
          />
        </label>
        <label className="block text-sm">
          To
          <input
            type="date"
            className="mt-1 w-full rounded border px-3 py-2"
            value={formTo}
            onChange={(e) => setFormTo(e.target.value)}
          />
        </label>
      </div>
      <button type="submit" className="rounded bg-black px-4 py-2 text-sm text-white">
        Search
      </button>
    </form>
  );
}

function EventsResultsPanel({
  q,
  from,
  to,
  page,
  onPageChange,
}: {
  q: string;
  from: string;
  to: string;
  page: number;
  onPageChange: (page: number) => void;
}) {
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [reload, setReload] = useState(0);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await listEvents({
          q: q || undefined,
          from: from || undefined,
          to: to || undefined,
          page,
          limit: 20,
        });
        if (cancelled) return;
        if (data.items.length === 0) {
          setState({ kind: "empty" });
        } else {
          setState({ kind: "ok", data });
        }
      } catch (e) {
        if (cancelled) return;
        const msg = e instanceof ApiError ? e.message : e instanceof Error ? e.message : "Unknown error";
        const code = e instanceof ApiError ? e.code : undefined;
        setState({ kind: "error", message: msg, code });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [q, from, to, page, reload]);

  if (state.kind === "loading") {
    return <p className="text-gray-500">Loading events…</p>;
  }

  if (state.kind === "error") {
    return (
      <div className="rounded border border-red-300 bg-red-50 p-4 space-y-2">
        <p className="font-medium text-red-700">
          {state.code === "VALIDATION_FAILED" ? "Invalid search parameters" : "Could not load events"}
        </p>
        <p className="text-sm text-red-600">{state.message}</p>
        <button type="button" onClick={() => setReload((n) => n + 1)} className="text-sm underline">
          Retry
        </button>
      </div>
    );
  }

  if (state.kind === "empty") {
    return <p className="text-gray-600">No events match your search. Try different dates or keywords.</p>;
  }

  const totalPages = Math.max(1, Math.ceil(state.data.total / state.data.limit));
  return (
    <>
      <ul className="space-y-3">
        {state.data.items.map((ev: EventListItem) => (
          <li key={ev.id} className="rounded border p-4 hover:bg-gray-50">
            <Link href={`/events/${ev.id}`} className="block space-y-1">
              <h2 className="font-semibold">{ev.title}</h2>
              <p className="text-sm text-gray-600">{ev.venue}</p>
              <p className="text-sm">
                Next: {formatDateTime(ev.next_showtime_at)} · from {formatBaht(ev.min_price_satang)}
              </p>
            </Link>
          </li>
        ))}
      </ul>
      <div className="flex items-center justify-between text-sm">
        <button
          type="button"
          disabled={page <= 1}
          className="rounded border px-3 py-1 disabled:opacity-40"
          onClick={() => onPageChange(page - 1)}
        >
          Previous
        </button>
        <span>
          Page {page} of {totalPages}
        </span>
        <button
          type="button"
          disabled={page >= totalPages}
          className="rounded border px-3 py-1 disabled:opacity-40"
          onClick={() => onPageChange(page + 1)}
        >
          Next
        </button>
      </div>
    </>
  );
}

function EventsContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const q = searchParams.get("q") ?? "";
  const from = searchParams.get("from") ?? "";
  const to = searchParams.get("to") ?? "";
  const page = Math.max(1, parseInt(searchParams.get("page") ?? "1", 10) || 1);

  const pushParams = (next: { q?: string; from?: string; to?: string; page?: number }) => {
    const sp = new URLSearchParams();
    const nq = next.q ?? q;
    const nf = next.from ?? from;
    const nt = next.to ?? to;
    const np = next.page ?? page;
    if (nq) sp.set("q", nq);
    if (nf) sp.set("from", nf);
    if (nt) sp.set("to", nt);
    if (np > 1) sp.set("page", String(np));
    const qs = sp.toString();
    router.push(`/events${qs ? `?${qs}` : ""}`);
  };

  return (
    <main className="mx-auto max-w-2xl p-6 space-y-6">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-bold">Events</h1>
        <Link href="/" className="text-sm text-gray-600 underline">
          Home
        </Link>
      </div>

      <SearchForm
        key={`${q}|${from}|${to}`}
        initialQ={q}
        initialFrom={from}
        initialTo={to}
        onSearch={(nq, nf, nt) => pushParams({ q: nq, from: nf, to: nt, page: 1 })}
      />

      <EventsResultsPanel
        key={`${q}|${from}|${to}|${page}`}
        q={q}
        from={from}
        to={to}
        page={page}
        onPageChange={(p) => pushParams({ page: p })}
      />
    </main>
  );
}

export default function EventsPage() {
  return (
    <Suspense fallback={<main className="p-6">Loading…</main>}>
      <EventsContent />
    </Suspense>
  );
}
