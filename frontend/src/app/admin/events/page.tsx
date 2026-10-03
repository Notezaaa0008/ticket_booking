"use client";

import { useEffect, useState, type FormEvent } from "react";
import { AdminGuard, errorText } from "@/components/AdminGuard";
import {
  Alert,
  Button,
  EmptyState,
  Field,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  Select,
  TableWrap,
  Td,
  Th,
} from "@/components/ui";
import {
  adminCreateEvent,
  adminCreateShowtime,
  adminDeleteEvent,
  adminListEvents,
  adminUpdateShowtime,
  type AdminEvent,
  type AdminShowtime,
} from "@/lib/api";
import { formatDateTime } from "@/lib/format";

type LoadState = { kind: "loading" } | { kind: "ok"; events: AdminEvent[] } | { kind: "error"; message: string };
type Msg = { ok: boolean; text: string } | null;

export default function AdminEventsPage() {
  return (
    <AdminGuard>
      <EventsAdmin />
    </AdminGuard>
  );
}

function EventsAdmin() {
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [tick, setTick] = useState(0);
  const [msg, setMsg] = useState<Msg>(null);
  const reload = () => setTick((n) => n + 1);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const events = await adminListEvents();
        if (!cancelled) setState({ kind: "ok", events });
      } catch (e) {
        if (!cancelled) setState({ kind: "error", message: errorText(e) });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [tick]);

  async function run(action: () => Promise<string>) {
    try {
      setMsg({ ok: true, text: await action() });
      reload();
    } catch (e) {
      setMsg({ ok: false, text: errorText(e) });
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader title="Events & showtimes" description="Create events, add showtimes, and manage sale status." />
      {msg && <Alert variant={msg.ok ? "success" : "error"}>{msg.text}</Alert>}
      <CreateEventForm onSubmit={(title, venue) => run(async () => {
        const e = await adminCreateEvent({ title, venue });
        return `Created event "${e.title}"`;
      })} />
      {state.kind === "loading" && <LoadingState message="Loading events…" />}
      {state.kind === "error" && (
        <Alert variant="error" onRetry={reload}>
          {state.message}
        </Alert>
      )}
      {state.kind === "ok" && state.events.length === 0 && (
        <EmptyState title="No events yet" description="Create your first event to start selling tickets." />
      )}
      {state.kind === "ok" &&
        state.events.map((ev) => (
          <Panel key={ev.id} className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p className="font-semibold text-slate-900">{ev.title}</p>
                <p className="text-sm text-slate-500">
                  {ev.venue} · {ev.status}
                </p>
              </div>
              <Button
                variant="secondary"
                className="px-3 py-1.5 text-xs"
                onClick={() => run(async () => {
                  const r = await adminDeleteEvent(ev.id);
                  return r.result === "archived" ? `Archived "${ev.title}" (it has showtimes)` : `Deleted "${ev.title}"`;
                })}
              >
                {ev.showtimes.length > 0 ? "Archive" : "Delete"}
              </Button>
            </div>
            {ev.showtimes.length === 0 ? (
              <p className="text-sm text-slate-500">No showtimes yet.</p>
            ) : (
              <TableWrap>
                <thead>
                  <tr>
                    <Th>Starts</Th>
                    <Th>Seats</Th>
                    <Th>Bookings</Th>
                    <Th>Status</Th>
                  </tr>
                </thead>
                <tbody>
                  {ev.showtimes.map((st) => (
                    <tr key={st.id}>
                      <Td>{formatDateTime(st.starts_at)}</Td>
                      <Td className="tabular-nums">{st.seat_count}</Td>
                      <Td className="tabular-nums">{st.booking_count}</Td>
                      <Td>
                        <Select
                          aria-label="Showtime status"
                          className="mt-0 max-w-[10rem] py-1.5"
                          value={st.status}
                          onChange={(e) => {
                            const status = e.target.value as AdminShowtime["status"];
                            run(async () => {
                              await adminUpdateShowtime(st.id, { status });
                              return `Showtime set to ${status}`;
                            });
                          }}
                        >
                          <option value="on_sale">on_sale</option>
                          <option value="closed">closed</option>
                          <option value="cancelled">cancelled</option>
                        </Select>
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </TableWrap>
            )}
            {ev.status !== "archived" && (
              <AddShowtimeForm
                onSubmit={(input) => run(async () => {
                  const st = await adminCreateShowtime(ev.id, input);
                  return `Added showtime with ${st.seat_count} seats`;
                })}
              />
            )}
          </Panel>
        ))}
    </div>
  );
}

function CreateEventForm({ onSubmit }: { onSubmit: (title: string, venue: string) => void }) {
  const [title, setTitle] = useState("");
  const [venue, setVenue] = useState("");
  function submit(e: FormEvent) {
    e.preventDefault();
    onSubmit(title, venue);
    setTitle("");
    setVenue("");
  }
  return (
    <Panel>
      <form onSubmit={submit} className="flex flex-wrap items-end gap-4">
        <p className="w-full text-sm font-semibold text-slate-900">Create event</p>
        <Field label="Title" className="min-w-[12rem] flex-1">
          <Input required value={title} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label="Venue" className="min-w-[12rem] flex-1">
          <Input required value={venue} onChange={(e) => setVenue(e.target.value)} />
        </Field>
        <Button type="submit" className="mb-0.5">
          Create
        </Button>
      </form>
    </Panel>
  );
}

function AddShowtimeForm({
  onSubmit,
}: {
  onSubmit: (input: { starts_at: string; rows: number; seats_per_row: number; price_satang: number }) => void;
}) {
  const [startsAt, setStartsAt] = useState("");
  const [rows, setRows] = useState(5);
  const [perRow, setPerRow] = useState(10);
  const [priceBaht, setPriceBaht] = useState(300);
  function submit(e: FormEvent) {
    e.preventDefault();
    onSubmit({
      starts_at: new Date(startsAt).toISOString(),
      rows,
      seats_per_row: perRow,
      price_satang: Math.round(priceBaht * 100),
    });
  }
  return (
    <form onSubmit={submit} className="flex flex-wrap items-end gap-3 border-t border-slate-100 pt-4">
      <Field label="Starts">
        <Input required type="datetime-local" value={startsAt} onChange={(e) => setStartsAt(e.target.value)} />
      </Field>
      <Field label="Rows">
        <Input type="number" min={1} max={20} className="w-20" value={rows} onChange={(e) => setRows(Number(e.target.value))} />
      </Field>
      <Field label="Seats / row">
        <Input type="number" min={1} max={30} className="w-20" value={perRow} onChange={(e) => setPerRow(Number(e.target.value))} />
      </Field>
      <Field label="Price (THB)">
        <Input type="number" min={0} step="0.01" className="w-28" value={priceBaht} onChange={(e) => setPriceBaht(Number(e.target.value))} />
      </Field>
      <Button type="submit" variant="secondary" className="mb-0.5">
        Add showtime
      </Button>
    </form>
  );
}
