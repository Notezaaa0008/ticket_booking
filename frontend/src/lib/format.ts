/** Money is stored as integer satang. Convert to a Thai baht string for display only. */
export function formatBaht(satang: number): string {
  return (satang / 100).toLocaleString("th-TH", { style: "currency", currency: "THB" });
}

/** Timestamps arrive in UTC; always show them in Bangkok time. */
export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("th-TH", {
    timeZone: "Asia/Bangkok",
    dateStyle: "medium",
    timeStyle: "short",
  });
}
