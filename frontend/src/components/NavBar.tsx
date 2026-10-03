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
        {state.status === "authenticated" && <Link href="/tickets">My tickets</Link>}
      </div>
      <div className="flex items-center gap-3">
        {state.status === "loading" && <span className="text-gray-500">…</span>}
        {state.status === "anonymous" && (
          <>
            <Link
              href="/login"
              className="rounded border border-gray-800 px-3 py-1 font-medium hover:bg-gray-50"
            >
              Login
            </Link>
            <Link
              href="/register"
              className="rounded bg-black px-3 py-1 font-medium text-white hover:bg-gray-800"
            >
              Register
            </Link>
          </>
        )}
        {state.status === "authenticated" && (
          <>
            <span className="text-gray-600" title={state.user.email}>
              {state.user.name}
              <span className="hidden sm:inline text-gray-500"> ({state.user.email})</span>
            </span>
            <button
              type="button"
              className="rounded border px-3 py-1"
              onClick={() => {
                logout();
                router.replace("/login");
              }}
            >
              Logout
            </button>
          </>
        )}
      </div>
    </nav>
  );
}
