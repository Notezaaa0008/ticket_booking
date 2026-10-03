"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useAuth } from "@/lib/auth";

/** Default entry: send visitors to login or the event list based on session. */
export default function Home() {
  const { state } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (state.status === "loading") return;
    router.replace(state.status === "authenticated" ? "/events" : "/login");
  }, [state.status, router]);

  return (
    <main className="mx-auto max-w-xl p-8">
      <p className="text-gray-500">Loading…</p>
    </main>
  );
}
