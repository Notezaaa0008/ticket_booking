"use client";

import { useEffect, useRef, useState } from "react";
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
  StatCard,
  TableWrap,
  Td,
  Th,
} from "@/components/ui";
import {
  adminListPayments,
  adminListRefunds,
  adminProcessRefund,
  adminRequestRefund,
  adminStats,
  type AdminPayment,
  type AdminStats,
  type Refund,
} from "@/lib/api";
import { formatBaht, formatDateTime } from "@/lib/format";

type Data = { stats: AdminStats; needsRefund: AdminPayment[]; refunds: Refund[] };
type LoadState = { kind: "loading" } | { kind: "ok"; data: Data } | { kind: "error"; message: string };

type RefundUi =
  | { kind: "idle" }
  | { kind: "confirm"; payment: AdminPayment }
  | { kind: "requesting"; payment: AdminPayment }
  | { kind: "processing"; payment: AdminPayment; refund: Refund }
  | { kind: "done"; payment: AdminPayment; refund: Refund }
  | { kind: "error"; payment: AdminPayment; message: string; refund?: Refund };

const REASONS = ["PAYMENT_NEEDS_REFUND", "CUSTOMER_REQUEST", "SHOWTIME_CANCELLED", "DUPLICATE_CHARGE"];

export default function AdminDashboardPage() {
  return (
    <AdminGuard>
      <Dashboard />
    </AdminGuard>
  );
}

function Dashboard() {
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [stats, needsRefund, refunds] = await Promise.all([
          adminStats(),
          adminListPayments("NEEDS_REFUND", 1),
          adminListRefunds("", 1),
        ]);
        if (!cancelled) setState({ kind: "ok", data: { stats, needsRefund: needsRefund.items, refunds: refunds.items } });
      } catch (e) {
        if (!cancelled) setState({ kind: "error", message: errorText(e) });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [tick]);

  if (state.kind === "loading") return <LoadingState message="Loading dashboard…" />;
  if (state.kind === "error")
    return (
      <Alert variant="error" title="Could not load the dashboard" onRetry={() => setTick((n) => n + 1)}>
        {state.message}
      </Alert>
    );

  const { stats, needsRefund, refunds } = state.data;
  return (
    <div className="space-y-8">
      <PageHeader title="Dashboard" description="Overview of revenue, bookings, and payments requiring attention." />

      <section className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <StatCard label="Revenue" value={formatBaht(stats.revenue_satang)} />
        <StatCard label="Tickets sold" value={String(stats.tickets_sold)} />
        <StatCard label="Bookings today" value={String(stats.bookings_today)} />
        <StatCard
          label="Needs refund"
          value={String(stats.needs_refund_count)}
          accent="danger"
          href="/admin/payments?status=NEEDS_REFUND"
          hrefLabel="View payments →"
        />
      </section>

      <section className="grid gap-4 sm:grid-cols-2">
        <Counts title="Bookings by status" counts={stats.bookings_by_status} />
        <Counts title="Payments by status" counts={stats.payments_by_status} />
      </section>

      <Panel className="space-y-4 ring-1 ring-rose-100">
        <h2 className="text-base font-semibold text-rose-900">Payments that need a refund ({needsRefund.length})</h2>
        {needsRefund.length === 0 ? (
          <EmptyState title="All clear" description="No payments are waiting for a refund right now." />
        ) : (
          <NeedsRefundTable payments={needsRefund} onChanged={() => setTick((n) => n + 1)} />
        )}
      </Panel>

      <section className="space-y-4">
        <h2 className="text-base font-semibold text-slate-900">Recent refunds</h2>
        {refunds.length === 0 ? (
          <EmptyState title="No refunds yet" description="Completed and failed refunds will appear here." />
        ) : (
          <TableWrap>
            <thead>
              <tr>
                <Th>Created</Th>
                <Th>Payment</Th>
                <Th>Amount</Th>
                <Th>Status</Th>
                <Th>Reason</Th>
                <Th>Failure</Th>
              </tr>
            </thead>
            <tbody>
              {refunds.map((r) => (
                <tr key={r.id}>
                  <Td>{formatDateTime(r.created_at)}</Td>
                  <Td className="font-mono text-xs">{r.payment_id.slice(0, 8)}</Td>
                  <Td className="tabular-nums">{formatBaht(r.amount_satang)}</Td>
                  <Td>{r.status}</Td>
                  <Td>{r.reason_code}</Td>
                  <Td>{r.failure_code ?? "—"}</Td>
                </tr>
              ))}
            </tbody>
          </TableWrap>
        )}
      </section>
    </div>
  );
}

function Counts({ title, counts }: { title: string; counts: Record<string, number> }) {
  const entries = Object.entries(counts);
  return (
    <Panel>
      <p className="text-sm font-semibold text-slate-900">{title}</p>
      {entries.length === 0 ? (
        <p className="mt-2 text-sm text-slate-500">None</p>
      ) : (
        <ul className="mt-3 space-y-1.5 text-sm text-slate-700">
          {entries.map(([k, v]) => (
            <li key={k} className="flex justify-between gap-4">
              <span className="text-slate-500">{k}</span>
              <span className="font-medium tabular-nums">{v}</span>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function NeedsRefundTable({ payments, onChanged }: { payments: AdminPayment[]; onChanged: () => void }) {
  const [ui, setUi] = useState<RefundUi>({ kind: "idle" });
  const [reason, setReason] = useState(REASONS[0]);
  const [note, setNote] = useState("");
  const keys = useRef<Record<string, string>>({});

  async function confirm(payment: AdminPayment) {
    setUi({ kind: "requesting", payment });
    if (!keys.current[payment.id]) keys.current[payment.id] = crypto.randomUUID();
    let refund: Refund;
    try {
      refund = await adminRequestRefund(payment.id, reason, note, keys.current[payment.id]);
    } catch (e) {
      delete keys.current[payment.id];
      setUi({ kind: "error", payment, message: errorText(e) });
      return;
    }
    setUi({ kind: "processing", payment, refund });
    try {
      const processed = await adminProcessRefund(refund.id);
      delete keys.current[payment.id];
      setUi({ kind: "done", payment, refund: processed });
    } catch (e) {
      setUi({ kind: "error", payment, refund, message: errorText(e) });
    }
  }

  return (
    <div className="space-y-4">
      <TableWrap>
        <thead>
          <tr>
            <Th>Paid</Th>
            <Th>User</Th>
            <Th>Event</Th>
            <Th>Amount</Th>
            <Th>Failure</Th>
            <Th />
          </tr>
        </thead>
        <tbody>
          {payments.map((p) => (
            <tr key={p.id}>
              <Td>{p.paid_at ? formatDateTime(p.paid_at) : "—"}</Td>
              <Td>{p.user_email}</Td>
              <Td>{p.event_title}</Td>
              <Td className="tabular-nums">{formatBaht(p.amount_satang)}</Td>
              <Td className="text-slate-600">
                {p.failure_code}
                {p.failure_message ? ` — ${p.failure_message}` : ""}
              </Td>
              <Td>
                <Button
                  variant="secondary"
                  className="px-2 py-1 text-xs"
                  disabled={ui.kind === "requesting" || ui.kind === "processing"}
                  onClick={() => {
                    setNote("");
                    setUi({ kind: "confirm", payment: p });
                  }}
                >
                  Refund
                </Button>
              </Td>
            </tr>
          ))}
        </tbody>
      </TableWrap>

      {ui.kind === "confirm" && (
        <div role="dialog" aria-label="Confirm refund">
          <Panel className="space-y-4">
            <p className="font-medium text-slate-900">
              Refund {formatBaht(ui.payment.amount_satang)} to {ui.payment.user_email}? This is a full refund.
            </p>
            <Field label="Reason">
              <Select value={reason} onChange={(e) => setReason(e.target.value)}>
                {REASONS.map((r) => (
                  <option key={r}>{r}</option>
                ))}
              </Select>
            </Field>
            <Field label="Note">
              <Input value={note} onChange={(e) => setNote(e.target.value)} />
            </Field>
            <div className="flex gap-2">
              <Button variant="danger" className="px-3 py-1.5" onClick={() => confirm(ui.payment)}>
                Confirm refund
              </Button>
              <Button variant="secondary" className="px-3 py-1.5" onClick={() => setUi({ kind: "idle" })}>
                Cancel
              </Button>
            </div>
          </Panel>
        </div>
      )}
      {ui.kind === "requesting" && <LoadingState message="Requesting refund…" />}
      {ui.kind === "processing" && (
        <Alert variant="warning">
          Refund requested ({ui.refund.id.slice(0, 8)}); processing with the provider…
        </Alert>
      )}
      {ui.kind === "done" && ui.refund.status === "COMPLETED" && (
        <Alert variant="success" title="Refund completed">
          Provider reference: {ui.refund.provider_ref}
          <button type="button" className="mt-2 block font-medium underline" onClick={onChanged}>
            Refresh dashboard
          </button>
        </Alert>
      )}
      {ui.kind === "done" && ui.refund.status === "FAILED" && (
        <Alert variant="error" title={`Refund failed (${ui.refund.failure_code})`}>
          The payment is unchanged. Click Refund again to retry with a new request.
        </Alert>
      )}
      {ui.kind === "done" && ui.refund.status === "REQUESTED" && (
        <Alert variant="warning">Refund is still requested; refresh to see the result.</Alert>
      )}
      {ui.kind === "error" && (
        <Alert
          variant="error"
          title={ui.refund ? `Refund ${ui.refund.id.slice(0, 8)} was requested but processing failed` : "Refund rejected"}
        >
          {ui.message}
        </Alert>
      )}
    </div>
  );
}
