"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";
import { LoadingState, PageShell } from "@/components/ui";
import { ApiError, type Outcome } from "@/lib/api";
import { useRequireAuth } from "@/lib/auth";

const ADMIN_LINKS: { href: string; label: string; exact?: boolean }[] = [
  { href: "/admin", label: "Dashboard", exact: true },
  { href: "/admin/events", label: "Events" },
  { href: "/admin/bookings", label: "Bookings" },
  { href: "/admin/payments", label: "Payments" },
  { href: "/admin/check-in", label: "Check-in" },
];

/** Renders children only for admins; anonymous users go to /login, non-admins to /events. */
export function AdminGuard({ children }: { children: ReactNode }) {
  const auth = useRequireAuth();
  const router = useRouter();
  const pathname = usePathname();
  useEffect(() => {
    if (auth.status === "authenticated" && auth.user.role !== "admin") router.replace("/events");
  }, [auth, router]);

  if (auth.status !== "authenticated" || auth.user.role !== "admin") {
    return (
      <PageShell>
        <LoadingState message="Checking admin access…" />
      </PageShell>
    );
  }

  return (
    <PageShell className="max-w-6xl space-y-8">
      <div className="border-b border-slate-200 pb-4">
        <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">Administration</p>
        <nav className="mt-3 flex flex-wrap gap-1">
          {ADMIN_LINKS.map(({ href, label, exact }) => {
            const active = exact ? pathname === href : pathname === href || pathname.startsWith(`${href}/`);
            return (
              <Link
                key={href}
                href={href}
                className={`rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                  active ? "bg-slate-900 text-white" : "text-slate-600 hover:bg-slate-100 hover:text-slate-900"
                }`}
              >
                {label}
              </Link>
            );
          })}
        </nav>
      </div>
      {children}
    </PageShell>
  );
}

const MESSAGES: Record<string, string> = {
  REFUND_NOT_ELIGIBLE: "This payment cannot be refunded (only SUCCEEDED or NEEDS_REFUND payments).",
  TICKET_ALREADY_USED: "A ticket of this booking was already used.",
  REFUND_ALREADY_EXISTS: "This payment already has an active refund.",
  AMOUNT_EXCEEDS_PAYMENT: "The refund would exceed the payment amount.",
  PROVIDER_FAILED: "The refund provider failed. Request a new refund to retry.",
  TICKET_VOID: "This ticket is void (refunded or its booking is not paid).",
  CANNOT_CANCEL_USED_SHOWTIME: "Cannot cancel this showtime because a ticket has already been checked in.",
  CANNOT_CANCEL_USED_EVENT: "Cannot cancel this event because a ticket has already been checked in.",
  NOT_FOUND: "Not found.",
  VALIDATION_FAILED: "The input is invalid.",
  FORBIDDEN: "Admin access required.",
  UNAUTHENTICATED: "Please log in again.",
};

/** Maps an API error to a specific message (code + server message), never a generic error. */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    const known = MESSAGES[e.code];
    return known ? `${known} (${e.code}: ${e.message})` : `${e.code}: ${e.message}`;
  }
  return e instanceof Error ? e.message : "Unknown error";
}

const OUTCOME_CLS: Record<Outcome, string> = {
  SUCCESS: "bg-emerald-50 text-emerald-800 ring-emerald-600/20",
  FAILURE: "bg-rose-50 text-rose-800 ring-rose-600/20",
  IGNORED: "bg-slate-100 text-slate-600 ring-slate-500/20",
};

export function OutcomeBadge({ outcome }: { outcome: Outcome }) {
  return (
    <span className={`inline-flex rounded-md px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${OUTCOME_CLS[outcome]}`}>
      {outcome}
    </span>
  );
}
