"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { ApiError } from "@/lib/api";
import { safeNext, useAuth } from "@/lib/auth";

function registerMessage(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case "EMAIL_TAKEN":
        return "This email is already registered. Try logging in instead.";
      case "VALIDATION_FAILED":
        return e.message;
      case "RATE_LIMITED":
        return "Too many attempts. Please wait a minute and try again.";
    }
    return e.message;
  }
  return "Could not reach the server. Please try again.";
}

export default function RegisterPage() {
  const { register } = useAuth();
  const router = useRouter();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const onSubmit = async (ev: FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await register(email, password, name);
      router.replace(safeNext(new URLSearchParams(window.location.search).get("next")));
    } catch (e) {
      setError(registerMessage(e));
      setBusy(false);
    }
  };

  return (
    <main className="mx-auto max-w-sm space-y-4 p-6">
      <h1 className="text-xl font-bold">Create an account</h1>
      <form onSubmit={onSubmit} className="space-y-3">
        <label className="block text-sm">
          Name
          <input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} className="mt-1 w-full rounded border px-3 py-2" autoComplete="name" />
        </label>
        <label className="block text-sm">
          Email
          <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} className="mt-1 w-full rounded border px-3 py-2" autoComplete="email" />
        </label>
        <label className="block text-sm">
          Password (at least 8 characters)
          <input type="password" required minLength={8} maxLength={72} value={password} onChange={(e) => setPassword(e.target.value)} className="mt-1 w-full rounded border px-3 py-2" autoComplete="new-password" />
        </label>
        {error && (
          <p role="alert" className="rounded border border-red-300 bg-red-50 p-2 text-sm text-red-700">
            {error}
          </p>
        )}
        <button type="submit" disabled={busy} className="w-full rounded bg-black px-4 py-2 text-sm font-medium text-white disabled:opacity-50">
          {busy ? "Creating account…" : "Register"}
        </button>
      </form>
      <p className="text-sm text-gray-600">
        Already registered?{" "}
        <Link href="/login" className="underline">
          Log in
        </Link>
      </p>
    </main>
  );
}
