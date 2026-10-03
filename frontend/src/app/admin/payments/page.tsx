"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { AdminGuard, errorText } from "@/components/AdminGuard";
import {
  Alert,
  EmptyState,
  Field,
  LoadingState,
  PageHeader,
  Pagination,
  Panel,
  Select,
  TableWrap,
  Td,
  Th,
} from "@/components/ui";
import { adminListPayments, type AdminPayment, type Page, type PaymentStatus } from "@/lib/api";
import { formatBaht, formatDateTime } from "@/lib/format";

type LoadState = { kind: "loading" } | { kind: "ok"; page: Page<AdminPayment> } | { kind: "error"; message: string };

const STATUSES: (PaymentStatus | "")[] = ["", "PENDING", "SUCCEEDED", "FAILED", "NEEDS_REFUND", "REFUNDED"];

function pageFromSearchParams(searchParams: URLSearchParams): number {
  const n = Number.parseInt(searchParams.get("page") ?? "1", 10);
  return Number.isFinite(n) && n >= 1 ? n : 1;
}

export default function AdminPaymentsPage() {
  return (
    <AdminGuard>
      <Suspense fallback={<LoadingState message="Loading payments…" />}>
        <PaymentsAdmin />
      </Suspense>
    </AdminGuard>
  );
}

function PaymentsAdmin() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const status = searchParams.get("status") ?? "";
  const page = pageFromSearchParams(searchParams);
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [tick, setTick] = useState(0);

  function pushQuery(next: { status?: string; page?: number }) {
    const sp = new URLSearchParams(searchParams.toString());
    if (next.status !== undefined) {
      if (next.status) sp.set("status", next.status);
      else sp.delete("status");
      sp.delete("page");
    }
    if (next.page !== undefined) {
      if (next.page <= 1) sp.delete("page");
      else sp.set("page", String(next.page));
    }
    const qs = sp.toString();
    router.push(qs ? `${pathname}?${qs}` : pathname);
  }

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await adminListPayments(status, page);
        if (!cancelled) setState({ kind: "ok", page: res });
      } catch (e) {
        if (!cancelled) setState({ kind: "error", message: errorText(e) });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [status, page, tick]);

  const totalPages = state.kind === "ok" ? Math.max(1, Math.ceil(state.page.total / state.page.limit)) : 1;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Payments"
        description="Review payment status and failure reasons across all bookings."
        action={
          <Link href="/admin" className="text-sm font-medium text-slate-600 hover:text-slate-900">
            ← Dashboard
          </Link>
        }
      />

      <Panel className="max-w-xs">
        <Field label="Filter by status">
          <Select value={status} onChange={(e) => pushQuery({ status: e.target.value })}>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {s || "All statuses"}
              </option>
            ))}
          </Select>
        </Field>
      </Panel>

      {status === "NEEDS_REFUND" && (
        <Alert variant="warning" title="Action required">
          These payments were collected but tickets could not be issued. Process a full refund from the{" "}
          <Link href="/admin" className="font-medium underline underline-offset-2">
            dashboard
          </Link>
          .
        </Alert>
      )}

      {state.kind === "loading" && <LoadingState message="Loading payments…" />}
      {state.kind === "error" && (
        <Alert variant="error" onRetry={() => setTick((n) => n + 1)}>
          {state.message}
        </Alert>
      )}
      {state.kind === "ok" && state.page.items.length === 0 && (
        <EmptyState
          title="No payments match this filter"
          description="Try another status or clear the filter to see all payments."
        />
      )}
      {state.kind === "ok" && state.page.items.length > 0 && (
        <>
          <TableWrap>
            <thead>
              <tr>
                <Th>Created</Th>
                <Th>User</Th>
                <Th>Event</Th>
                <Th>Amount</Th>
                <Th>Status</Th>
                <Th>Booking</Th>
                <Th>Failure</Th>
              </tr>
            </thead>
            <tbody>
              {state.page.items.map((p) => (
                <tr key={p.id} className="hover:bg-slate-50/80">
                  <Td>{formatDateTime(p.created_at)}</Td>
                  <Td>{p.user_email ?? "—"}</Td>
                  <Td>{p.event_title ?? "—"}</Td>
                  <Td className="font-medium tabular-nums">{formatBaht(p.amount_satang)}</Td>
                  <Td>{p.status}</Td>
                  <Td>
                    {p.booking_status ?? "—"}
                    {p.booking_id ? (
                      <>
                        {" "}
                        <Link href="/admin/bookings" className="font-mono text-xs text-slate-900 underline">
                          {p.booking_id.slice(0, 8)}
                        </Link>
                      </>
                    ) : null}
                  </Td>
                  <Td className="text-slate-600">
                    {p.failure_code}
                    {p.failure_message ? ` — ${p.failure_message}` : ""}
                  </Td>
                </tr>
              ))}
            </tbody>
          </TableWrap>
          <Pagination
            page={page}
            totalPages={totalPages}
            onPrev={() => pushQuery({ page: page - 1 })}
            onNext={() => pushQuery({ page: page + 1 })}
          />
        </>
      )}
    </div>
  );
}
