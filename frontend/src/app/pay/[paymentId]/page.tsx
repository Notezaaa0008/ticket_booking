"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { ApiError, createPayment, getPayment, mockPay, type Payment } from "@/lib/api";
import { useAuth, useRequireAuth } from "@/lib/auth";
import { formatBaht } from "@/lib/format";

type LoadState =
  | { kind: "loading" }
  | { kind: "ok"; payment: Payment }
  | { kind: "not_found" }
  | { kind: "error"; message: string };

const POLL_MS = 2000;
const SUCCESS_REDIRECT_MS = 1500;

export default function MockPayPage() {
  const params = useParams();
  const paymentId = typeof params.paymentId === "string" ? params.paymentId : "";
  const auth = useRequireAuth();
  const { logout } = useAuth();
  const authenticated = auth.status === "authenticated";
  const router = useRouter();

  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [tick, setTick] = useState(0);
  const [submitted, setSubmitted] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [retrying, setRetrying] = useState(false);
  const retryKey = useRef<string | null>(null);
  const simulateBusy = useRef(false);
  const redirectToTicketsDone = useRef(false);

  const status = state.kind === "ok" ? state.payment.status : null;

  useEffect(() => {
    if (!authenticated || !paymentId) return;
    let cancelled = false;
    (async () => {
      try {
        const payment = await getPayment(paymentId);
        if (!cancelled) setState({ kind: "ok", payment });
      } catch (e) {
        if (cancelled) return;
        if (e instanceof ApiError && e.status === 401) {
          logout();
          return;
        }
        if (e instanceof ApiError && e.status === 404) {
          setState({ kind: "not_found" });
          return;
        }
        setState({ kind: "error", message: e instanceof Error ? e.message : "Unknown error" });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [authenticated, paymentId, tick, logout]);

  // The result arrives asynchronously through the webhook: poll while the payment is PENDING.
  useEffect(() => {
    if (status !== "PENDING") return;
    const poll = window.setInterval(() => setTick((n) => n + 1), POLL_MS);
    return () => window.clearInterval(poll);
  }, [status]);

  // After SUCCEEDED (poll or mock gateway), show a short confirmation then open My tickets (fresh list from API).
  useEffect(() => {
    if (state.kind !== "ok" || state.payment.status !== "SUCCEEDED") return;
    if (redirectToTicketsDone.current) return;
    redirectToTicketsDone.current = true;
    const timer = window.setTimeout(() => router.replace("/tickets"), SUCCESS_REDIRECT_MS);
    return () => window.clearTimeout(timer);
  }, [state, router]);

  const onSimulate = async (result: "success" | "fail") => {
    if (simulateBusy.current) return;
    simulateBusy.current = true;
    setSubmitted(true);
    setActionError(null);
    try {
      await mockPay(paymentId, result);
      const payment = await getPayment(paymentId);
      setState({ kind: "ok", payment });
    } catch (e) {
      setSubmitted(false);
      simulateBusy.current = false;
      if (e instanceof ApiError && e.status === 404) {
        setActionError("The mock gateway is not enabled on this server.");
      } else {
        setActionError(e instanceof Error ? e.message : "The gateway could not be reached.");
      }
    }
    setTick((n) => n + 1);
  };

  const onRetry = async (bookingId: string) => {
    setRetrying(true);
    setActionError(null);
    try {
      if (!retryKey.current) retryKey.current = crypto.randomUUID();
      const payment = await createPayment(bookingId, retryKey.current);
      router.push(payment.pay_url);
    } catch (e) {
      setRetrying(false);
      if (e instanceof ApiError && e.code === "BOOKING_EXPIRED") {
        setActionError("The booking has expired, so it can no longer be paid.");
      } else if (e instanceof ApiError && e.code === "BOOKING_NOT_PAYABLE") {
        setActionError(`The booking cannot be paid: ${e.message}.`);
      } else {
        setActionError(e instanceof Error ? e.message : "Could not start a new payment.");
      }
    }
  };

  return (
    <main className="mx-auto max-w-xl space-y-4 p-6">
      <h1 className="text-xl font-bold">Mock payment gateway</h1>
      {(!authenticated || state.kind === "loading") && <p className="text-gray-500">Loading payment…</p>}
      {authenticated && state.kind === "not_found" && <p className="text-gray-600">Payment not found.</p>}
      {authenticated && state.kind === "error" && (
        <div className="space-y-2 rounded border border-red-300 bg-red-50 p-4">
          <p className="font-medium text-red-700">Could not load the payment</p>
          <p className="text-sm text-red-600">{state.message}</p>
          <button type="button" className="text-sm underline" onClick={() => setTick((n) => n + 1)}>
            Retry
          </button>
        </div>
      )}
      {authenticated && state.kind === "ok" && (
        <>
          <p className="text-2xl font-bold">{formatBaht(state.payment.amount_satang)}</p>
          {actionError && (
            <p role="alert" className="rounded border border-red-300 bg-red-50 p-2 text-sm text-red-700">
              {actionError}
            </p>
          )}

          {state.payment.status === "PENDING" && !submitted && (
            <div className="flex gap-2">
              <button
                type="button"
                disabled={submitted}
                onClick={() => onSimulate("success")}
                className="rounded bg-green-700 px-4 py-2 text-sm text-white disabled:opacity-50"
              >
                Pay successfully
              </button>
              <button
                type="button"
                disabled={submitted}
                onClick={() => onSimulate("fail")}
                className="rounded border border-red-500 px-4 py-2 text-sm text-red-700 disabled:opacity-50"
              >
                Simulate failure
              </button>
            </div>
          )}

          {state.payment.status === "PENDING" && submitted && (
            <div className="rounded border border-yellow-400 bg-yellow-50 p-4" aria-live="polite">
              <p className="font-medium text-yellow-900">Processing payment…</p>
              <p className="text-sm text-yellow-800">Waiting for the gateway confirmation. This page updates automatically.</p>
            </div>
          )}

          {state.payment.status === "SUCCEEDED" && (
            <div className="space-y-1 rounded border border-green-400 bg-green-50 p-4" aria-live="polite">
              <p className="font-medium text-green-800">Payment successful</p>
              <p className="text-sm text-green-700">
                Your tickets were issued. Opening My tickets…
              </p>
              <Link href="/tickets" className="text-sm underline">
                View my tickets now
              </Link>
            </div>
          )}

          {state.payment.status === "FAILED" && (
            <div className="space-y-2 rounded border border-red-400 bg-red-50 p-4" aria-live="polite">
              <p className="font-medium text-red-800">Payment failed</p>
              <p className="text-sm text-red-700">
                {state.payment.failure_message || "The payment did not go through."}
                {state.payment.failure_code ? ` (${state.payment.failure_code})` : ""}
              </p>
              <p className="text-sm text-red-700">Your seats are still held until the booking expires.</p>
              <button
                type="button"
                disabled={retrying}
                onClick={() => onRetry(state.payment.booking_id)}
                className="rounded bg-black px-4 py-2 text-sm text-white disabled:opacity-50"
              >
                {retrying ? "Starting new payment…" : "Retry payment"}
              </button>
            </div>
          )}

          {state.payment.status === "NEEDS_REFUND" && (
            <div className="space-y-1 rounded border border-orange-400 bg-orange-50 p-4" aria-live="polite">
              <p className="font-medium text-orange-900">Payment received but tickets could not be issued, we will refund</p>
              <p className="text-sm text-orange-800">{state.payment.failure_message}</p>
              <p className="text-sm text-orange-800">
                Keep this payment id for support: <span className="font-mono">{state.payment.id}</span>
              </p>
            </div>
          )}

          {state.payment.status === "REFUNDED" && (
            <div className="rounded border border-blue-400 bg-blue-50 p-4">
              <p className="font-medium text-blue-800">Payment refunded</p>
            </div>
          )}

          <Link href={`/bookings/${state.payment.booking_id}`} className="block text-sm text-gray-600 underline">
            Back to booking
          </Link>
        </>
      )}
    </main>
  );
}
