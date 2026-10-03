"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";

export function NavBar() {
  const { state, logout } = useAuth();
  const router = useRouter();

  return (
    <nav className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3 text-sm">
      <div className="flex gap-4">
        <Link href="/events" className="font-medium">
          Events
        </Link>
        {state.status === "authenticated" && <Link href="/bookings">My bookings</Link>}
      </div>
      <div className="flex items-center gap-3">
        {state.status === "loading" && <span className="text-gray-500">…</span>}
        {state.status === "anonymous" && (
          <>
            <Link href="/login" className="underline">
              Log in
            </Link>
            <Link href="/register" className="underline">
              Register
            </Link>
          </>
        )}
        {state.status === "authenticated" && (
          <>
            <span className="text-gray-600">{state.user.name}</span>
            <button
              type="button"
              className="rounded border px-3 py-1"
              onClick={() => {
                logout();
                router.push("/login");
              }}
            >
              Log out
            </button>
          </>
        )}
      </div>
    </nav>
  );
}
