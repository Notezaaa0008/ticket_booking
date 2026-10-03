"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { LoadingState, PageShell } from "@/components/ui";
import { resolveRootRedirect, useAuth } from "@/lib/auth";

/** Root URL: `/events` for guests and users, `/admin` for admins (no standalone landing page). */
export default function Home() {
  const { state } = useAuth();
  const router = useRouter();

  useEffect(() => {
    const target = resolveRootRedirect(state);
    if (target) router.replace(target);
  }, [state, router]);

  return (
    <PageShell>
      <LoadingState />
    </PageShell>
  );
}
