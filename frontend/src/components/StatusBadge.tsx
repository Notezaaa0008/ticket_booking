import type { BookingStatus } from "@/lib/api";

const STYLES: Record<BookingStatus, { label: string; cls: string }> = {
  PENDING: { label: "Pending payment", cls: "bg-amber-50 text-amber-900 ring-amber-600/20" },
  PAID: { label: "Paid", cls: "bg-emerald-50 text-emerald-800 ring-emerald-600/20" },
  EXPIRED: { label: "Expired", cls: "bg-slate-100 text-slate-600 ring-slate-500/20" },
  CANCELLED: { label: "Cancelled", cls: "bg-rose-50 text-rose-800 ring-rose-600/20" },
  REFUNDED: { label: "Refunded", cls: "bg-sky-50 text-sky-800 ring-sky-600/20" },
};

export function StatusBadge({ status }: { status: BookingStatus }) {
  const s = STYLES[status];
  return (
    <span className={`inline-flex rounded-md px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${s.cls}`}>{s.label}</span>
  );
}
