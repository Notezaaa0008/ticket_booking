import type { BookingStatus } from "@/lib/api";

const STYLES: Record<BookingStatus, { label: string; cls: string }> = {
  PENDING: { label: "Pending payment", cls: "bg-yellow-100 text-yellow-800 border-yellow-400" },
  PAID: { label: "Paid", cls: "bg-green-100 text-green-800 border-green-400" },
  EXPIRED: { label: "Expired", cls: "bg-gray-100 text-gray-700 border-gray-400" },
  CANCELLED: { label: "Cancelled", cls: "bg-red-100 text-red-800 border-red-400" },
  REFUNDED: { label: "Refunded", cls: "bg-blue-100 text-blue-800 border-blue-400" },
};

export function StatusBadge({ status }: { status: BookingStatus }) {
  const s = STYLES[status];
  return <span className={`rounded border px-2 py-0.5 text-xs font-medium ${s.cls}`}>{s.label}</span>;
}
