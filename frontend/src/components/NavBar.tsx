"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { Button } from "@/components/ui";

function NavLink({ href, children }: { href: string; children: React.ReactNode }) {
  const pathname = usePathname();
  const active = pathname === href || (href !== "/events" && pathname.startsWith(href));
  return (
    <Link
      href={href}
      className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
        active ? "bg-slate-100 text-slate-900" : "text-slate-600 hover:bg-slate-50 hover:text-slate-900"
      }`}
    >
      {children}
    </Link>
  );
}

export function NavBar() {
  const { state, logout } = useAuth();
  const router = useRouter();
  const pathname = usePathname();
  const onAuthPage = pathname === "/login" || pathname === "/register";

  return (
    <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/90 backdrop-blur-md">
      <nav className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6">
        <div className="flex items-center gap-1">
          <Link href="/events" className="mr-3 text-sm font-semibold tracking-tight text-slate-900">
            Ticket Booking
          </Link>
          {!onAuthPage && (
            <>
              <NavLink href="/events">Events</NavLink>
              {state.status === "authenticated" && <NavLink href="/bookings">My bookings</NavLink>}
              {state.status === "authenticated" && <NavLink href="/tickets">My tickets</NavLink>}
              {state.status === "authenticated" && state.user.role === "admin" && <NavLink href="/admin">Admin</NavLink>}
            </>
          )}
        </div>
        <div className="flex items-center gap-2">
          {state.status === "loading" && <span className="text-sm text-slate-400">…</span>}
          {state.status === "anonymous" && (
            <>
              <Link
                href="/login"
                className="inline-flex rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
              >
                Log in
              </Link>
              <Link
                href="/register"
                className="inline-flex rounded-lg bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800"
              >
                Register
              </Link>
            </>
          )}
          {state.status === "authenticated" && (
            <>
              <span className="hidden max-w-[12rem] truncate text-sm text-slate-600 sm:inline" title={state.user.email}>
                {state.user.name}
              </span>
              <Button
                variant="secondary"
                className="px-3 py-1.5"
                onClick={() => {
                  logout();
                  router.replace("/login");
                }}
              >
                Log out
              </Button>
            </>
          )}
        </div>
      </nav>
    </header>
  );
}
