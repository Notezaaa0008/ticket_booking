"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { Alert, AuthLayout, Button, Field, Input } from "@/components/ui";
import { ApiError } from "@/lib/api";
import { postLoginRedirect, useAuth } from "@/lib/auth";

function loginMessage(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case "INVALID_CREDENTIALS":
        return "Wrong email or password.";
      case "RATE_LIMITED":
        return "Too many login attempts. Please wait a minute and try again.";
      case "VALIDATION_FAILED":
        return e.message;
      case "TIMEOUT":
        return "The server took too long to respond. Please try again.";
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
      const user = await login(email, password);
      const next = new URLSearchParams(window.location.search).get("next");
      router.replace(postLoginRedirect(user, next));
    } catch (e) {
      console.error("login failed", e);
      setError(loginMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthLayout title="Welcome back" subtitle="Sign in to book tickets and manage your orders.">
      <form onSubmit={onSubmit} className="space-y-4">
        <Field label="Email">
          <Input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" />
        </Field>
        <Field label="Password">
          <Input
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </Field>
        {error ? <Alert variant="error">{error}</Alert> : null}
        <Button type="submit" disabled={busy} className="w-full">
          {busy ? "Logging in…" : "Log in"}
        </Button>
      </form>
      <p className="mt-6 text-center text-sm text-slate-500">
        No account?{" "}
        <Link href="/register" className="font-medium text-slate-900 hover:underline">
          Create one
        </Link>
      </p>
    </AuthLayout>
  );
}
