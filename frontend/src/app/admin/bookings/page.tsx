"use client";

import { useEffect, useState } from "react";
import { AdminGuard, errorText, OutcomeBadge } from "@/components/AdminGuard";
import { StatusBadge } from "@/components/StatusBadge";
import {
  Alert,
  Button,
  EmptyState,
  Field,
  Input,
  LoadingState,
  PageHeader,
  Pagination,
  Panel,
  Select,
  TableWrap,
  Td,
  Th,
} from "@/components/ui";
import {
  adminBookingTimeline,
  adminListBookings,
  type AdminBooking,
  type BookingTimeline,
  type Page,
} from "@/lib/api";
import { formatBaht, formatDateTime } from "@/lib/format";

type LoadState = { kind: "loading" } | { kind: "ok"; page: Page<AdminBooking> } | { kind: "error"; message: string };
type TimelineState =
  | { kind: "none" }
  | { kind: "loading"; id: string }
  | { kind: "ok"; data: BookingTimeline }
  | { kind: "error"; id: string; message: string };

const STATUSES = ["", "PENDING", "PAID", "EXPIRED", "CANCELLED", "REFUNDED"];

export default function AdminBookingsPage() {
  return (
    <AdminGuard>
      <BookingsAdmin />
    </AdminGuard>
  );
}

function BookingsAdmin() {
  const [status, setStatus] = useState("");
  const [email, setEmail] = useState("");
  const [showtimeId, setShowtimeId] = useState("");
  const [page, setPage] = useState(1);
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [tick, setTick] = useState(0);
  const [timeline, setTimeline] = useState<TimelineState>({ kind: "none" });

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await adminListBookings({
          status: status || undefined,
          email: email.trim() || undefined,
          showtime_id: showtimeId.trim() || undefined,
          page,
        });
        if (!cancelled) setState({ kind: "ok", page: res });
      } catch (e) {
        if (!cancelled) setState({ kind: "error", message: errorText(e) });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [status, email, showtimeId, page, tick]);

  async function openTimeline(id: string) {
    setTimeline({ kind: "loading", id });
    try {
      setTimeline({ kind: "ok", data: await adminBookingTimeline(id) });
    } catch (e) {
      setTimeline({ kind: "error", id, message: errorText(e) });
    }
  }

  const totalPages = state.kind === "ok" ? Math.max(1, Math.ceil(state.page.total / state.page.limit)) : 1;

  return (
    <div className="space-y-6">
      <PageHeader title="Bookings" description="Search bookings and open a row to inspect the full audit timeline." />

      <Panel className="flex flex-wrap items-end gap-4">
        <Field label="Status" className="min-w-[10rem]">
          <Select
            value={status}
            onChange={(e) => {
              setStatus(e.target.value);
              setPage(1);
            }}
          >
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {s || "All"}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="User email" className="min-w-[12rem] flex-1">
          <Input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Exact email match" />
        </Field>
        <Field label="Showtime id" className="min-w-[14rem] flex-1">
          <Input className="font-mono text-xs" value={showtimeId} onChange={(e) => setShowtimeId(e.target.value)} />
        </Field>
        <Button
          variant="secondary"
          className="mb-0.5"
          onClick={() => {
            setPage(1);
            setTick((n) => n + 1);
          }}
        >
          Apply filters
        </Button>
      </Panel>

      {state.kind === "loading" && <LoadingState message="Loading bookings…" />}
      {state.kind === "error" && (
        <Alert variant="error" onRetry={() => setTick((n) => n + 1)}>
          {state.message}
        </Alert>
      )}
      {state.kind === "ok" && state.page.items.length === 0 && (
        <EmptyState title="No bookings found" description="Adjust filters or wait for new reservations." />
      )}
      {state.kind === "ok" && state.page.items.length > 0 && (
        <>
          <TableWrap>
            <thead>
              <tr>
                <Th>Created</Th>
                <Th>User</Th>
                <Th>Showtime</Th>
                <Th>Seats</Th>
                <Th>Status</Th>
                <Th>Total</Th>
                <Th>Latest payment</Th>
              </tr>
            </thead>
            <tbody>
              {state.page.items.map((b) => (
                <tr
                  key={b.id}
                  className="cursor-pointer hover:bg-slate-50/80"
                  onClick={() => openTimeline(b.id)}
                >
                  <Td>{formatDateTime(b.created_at)}</Td>
                  <Td>{b.user_email}</Td>
                  <Td>
                    {b.event_title} · {formatDateTime(b.starts_at)}
                  </Td>
                  <Td>{b.seats.join(", ")}</Td>
                  <Td>
                    <StatusBadge status={b.status} />
                  </Td>
                  <Td className="tabular-nums">{formatBaht(b.total_satang)}</Td>
                  <Td>{b.latest_payment_status || "—"}</Td>
                </tr>
              ))}
            </tbody>
          </TableWrap>
          <Pagination page={page} totalPages={totalPages} onPrev={() => setPage((p) => p - 1)} onNext={() => setPage((p) => p + 1)} />
        </>
      )}
      <TimelinePanel state={timeline} onClose={() => setTimeline({ kind: "none" })} />
    </div>
  );
}

function TimelinePanel({ state, onClose }: { state: TimelineState; onClose: () => void }) {
  if (state.kind === "none") return null;
  return (
    <Panel className="space-y-4 ring-2 ring-slate-200" aria-label="Booking timeline">
      <div className="flex justify-between gap-4">
        <h2 className="text-base font-semibold text-slate-900">Booking timeline</h2>
        <Button variant="ghost" className="px-2 py-1" onClick={onClose}>
          Close
        </Button>
      </div>
      {state.kind === "loading" && <LoadingState message="Loading timeline…" />}
      {state.kind === "error" && <Alert variant="error">{state.message}</Alert>}
      {state.kind === "ok" && (
        <>
          <p>
            Booking <span className="font-mono">{state.data.booking.id}</span> · {state.data.booking.user_email} ·{" "}
            <StatusBadge status={state.data.booking.status} /> · {formatBaht(state.data.booking.total_satang)}
          </p>
          <div>
            <p className="font-medium">Payments</p>
            {state.data.payments.length === 0 ? (
              <p className="text-gray-500">No payments.</p>
            ) : (
              <ul>
                {state.data.payments.map((p) => (
                  <li key={p.id}>
                    <span className="font-mono text-xs">{p.id.slice(0, 8)}</span> {p.status} · {formatBaht(p.amount_satang)}
                    {p.provider_ref ? ` · ref ${p.provider_ref}` : ""}
                    {p.failure_code ? ` · ${p.failure_code}` : ""}
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div>
            <p className="font-medium">Refunds</p>
            {state.data.refunds.length === 0 ? (
              <p className="text-gray-500">No refunds.</p>
            ) : (
              <ul>
                {state.data.refunds.map((r) => (
                  <li key={r.id}>
                    {r.status} · {formatBaht(r.amount_satang)} · {r.reason_code}
                    {r.failure_code ? ` · ${r.failure_code}` : ""}
                    {r.provider_ref ? ` · ref ${r.provider_ref}` : ""}
                  </li>
                ))}
              </ul>
            )}
          </div>
          {state.data.events.length === 0 ? (
            <p className="text-gray-500">No events.</p>
          ) : (
            <TableWrap>
              <thead>
                <tr>
                  <Th>Time</Th>
                  <Th>Source</Th>
                  <Th>Event</Th>
                  <Th>Outcome</Th>
                  <Th>Reason</Th>
                  <Th>Actor</Th>
                  <Th>Status change</Th>
                </tr>
              </thead>
              <tbody>
                {state.data.events.map((e, i) => (
                  <tr key={i}>
                    <Td>{formatDateTime(e.created_at)}</Td>
                    <Td>{e.source}</Td>
                    <Td>{e.event_type}</Td>
                    <Td>
                      <OutcomeBadge outcome={e.outcome} />
                    </Td>
                    <Td>{e.reason_code || "—"}</Td>
                    <Td>{e.actor_type}</Td>
                    <Td>
                      {e.from_status || "—"} → {e.to_status || "—"}
                    </Td>
                  </tr>
                ))}
              </tbody>
            </TableWrap>
          )}
        </>
      )}
    </Panel>
  );
}
