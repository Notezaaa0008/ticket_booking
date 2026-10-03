"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { Alert, AuthLayout, Button, Field, Input } from "@/components/ui";
import { ApiError } from "@/lib/api";
import { postLoginRedirect, useAuth } from "@/lib/auth";

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
      const user = await register(email, password, name);
      const next = new URLSearchParams(window.location.search).get("next");
      router.replace(postLoginRedirect(user, next));
    } catch (e) {
      setError(registerMessage(e));
      setBusy(false);
    }
  };

  return (
    <AuthLayout title="Create an account" subtitle="Join to reserve seats and receive digital tickets.">
      <form onSubmit={onSubmit} className="space-y-4">
        <Field label="Name">
          <Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" />
        </Field>
        <Field label="Email">
          <Input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" />
        </Field>
        <Field label="Password">
          <Input
            type="password"
            required
            minLength={8}
            maxLength={72}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            placeholder="At least 8 characters"
          />
        </Field>
        {error && <Alert variant="error">{error}</Alert>}
        <Button type="submit" disabled={busy} className="w-full">
          {busy ? "Creating account…" : "Register"}
        </Button>
      </form>
      <p className="mt-6 text-center text-sm text-slate-500">
        Already registered?{" "}
        <Link href="/login" className="font-medium text-slate-900 hover:underline">
          Log in
        </Link>
      </p>
    </AuthLayout>
  );
}
