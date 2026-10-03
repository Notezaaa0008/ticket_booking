"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import {
  Alert,
  Button,
  EmptyState,
  Field,
  Input,
  LoadingState,
  PageHeader,
  PageShell,
  Pagination,
  Panel,
} from "@/components/ui";
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
    <Panel>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onSearch(formQ.trim(), formFrom, formTo);
        }}
        className="space-y-4"
      >
        <Field label="Search">
          <Input value={formQ} onChange={(e) => setFormQ(e.target.value)} placeholder="Title or venue" />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="From">
            <Input type="date" value={formFrom} onChange={(e) => setFormFrom(e.target.value)} />
          </Field>
          <Field label="To">
            <Input type="date" value={formTo} onChange={(e) => setFormTo(e.target.value)} />
          </Field>
        </div>
        <Button type="submit">Search events</Button>
      </form>
    </Panel>
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
    return <LoadingState message="Loading events…" />;
  }

  if (state.kind === "error") {
    return (
      <Alert
        variant="error"
        title={state.code === "VALIDATION_FAILED" ? "Invalid search parameters" : "Could not load events"}
        onRetry={() => setReload((n) => n + 1)}
      >
        {state.message}
      </Alert>
    );
  }

  if (state.kind === "empty") {
    return (
      <EmptyState
        title="No events found"
        description="Try different dates or keywords, or check back later for new showtimes."
      />
    );
  }

  const totalPages = Math.max(1, Math.ceil(state.data.total / state.data.limit));
  return (
    <div className="space-y-4">
      <ul className="space-y-3">
        {state.data.items.map((ev: EventListItem) => (
          <li key={ev.id}>
            <Link
              href={`/events/${ev.id}`}
              className="block rounded-xl border border-slate-200 bg-white p-5 shadow-sm transition-shadow hover:shadow-md"
            >
              <h2 className="text-lg font-semibold text-slate-900">{ev.title}</h2>
              <p className="mt-1 text-sm text-slate-500">{ev.venue}</p>
              <p className="mt-3 text-sm text-slate-700">
                Next show · {formatDateTime(ev.next_showtime_at)} · from {formatBaht(ev.min_price_satang)}
              </p>
            </Link>
          </li>
        ))}
      </ul>
      <Pagination page={page} totalPages={totalPages} onPrev={() => onPageChange(page - 1)} onNext={() => onPageChange(page + 1)} />
    </div>
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
    <PageShell className="max-w-2xl space-y-6">
      <PageHeader title="Events" description="Browse upcoming shows and pick a time that works for you." />

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
    </PageShell>
  );
}

export default function EventsPage() {
  return (
    <Suspense
      fallback={
        <PageShell>
          <LoadingState />
        </PageShell>
      }
    >
      <EventsContent />
    </Suspense>
  );
}
