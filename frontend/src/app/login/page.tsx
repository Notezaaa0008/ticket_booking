"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { ApiError } from "@/lib/api";
import { safeNext, useAuth } from "@/lib/auth";

function loginMessage(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case "INVALID_CREDENTIALS":
        return "Wrong email or password.";
      case "RATE_LIMITED":
        return "Too many login attempts. Please wait a minute and try again.";
      case "VALIDATION_FAILED":
        return e.message;
    }
    return e.message;
  }
  return "Could not reach the server. Please try again.";
}

export default function LoginPage() {
  const { login } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const onSubmit = async (ev: FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(email, password);
      router.replace(safeNext(new URLSearchParams(window.location.search).get("next")));
    } catch (e) {
      setError(loginMessage(e));
      setBusy(false);
    }
  };

  return (
    <main className="mx-auto max-w-sm space-y-4 p-6">
      <h1 className="text-xl font-bold">Log in</h1>
      <form onSubmit={onSubmit} className="space-y-3">
        <label className="block text-sm">
          Email
          <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} className="mt-1 w-full rounded border px-3 py-2" autoComplete="email" />
        </label>
        <label className="block text-sm">
          Password
          <input type="password" required value={password} onChange={(e) => setPassword(e.target.value)} className="mt-1 w-full rounded border px-3 py-2" autoComplete="current-password" />
        </label>
        {error && (
          <p role="alert" className="rounded border border-red-300 bg-red-50 p-2 text-sm text-red-700">
            {error}
          </p>
        )}
        <button type="submit" disabled={busy} className="w-full rounded bg-black px-4 py-2 text-sm font-medium text-white disabled:opacity-50">
          {busy ? "Logging in…" : "Log in"}
        </button>
      </form>
      <p className="text-sm text-gray-600">
        No account?{" "}
        <Link href="/register" className="underline">
          Register
        </Link>
      </p>
    </main>
  );
}
