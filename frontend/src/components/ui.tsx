import Link from "next/link";
import type {
  ButtonHTMLAttributes,
  HTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
} from "react";

export function PageShell({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`mx-auto w-full max-w-5xl px-4 py-8 sm:px-6 ${className}`}>{children}</div>;
}

export function PageHeader({
  title,
  description,
  action,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="mb-8 flex flex-wrap items-start justify-between gap-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight text-slate-900">{title}</h1>
        {description ? <p className="mt-1 text-sm text-slate-500">{description}</p> : null}
      </div>
      {action}
    </div>
  );
}

export function Panel({
  children,
  className = "",
  ...props
}: { children: ReactNode; className?: string } & Omit<HTMLAttributes<HTMLElement>, "className">) {
  return (
    <section className={`rounded-xl border border-slate-200 bg-white p-5 shadow-sm ${className}`} {...props}>
      {children}
    </section>
  );
}

export function StatCard({
  label,
  value,
  href,
  hrefLabel,
  accent,
}: {
  label: string;
  value: string;
  href?: string;
  hrefLabel?: string;
  accent?: "default" | "danger";
}) {
  const accentCls =
    accent === "danger" ? "border-rose-200 bg-rose-50/50 ring-1 ring-rose-100" : "border-slate-200 bg-white";
  return (
    <div className={`rounded-xl border p-5 shadow-sm ${accentCls}`}>
      <p className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</p>
      <p className="mt-2 text-2xl font-semibold tabular-nums text-slate-900">{value}</p>
      {href && hrefLabel ? (
        <Link href={href} className="mt-3 inline-block text-sm font-medium text-rose-700 hover:text-rose-800">
          {hrefLabel}
        </Link>
      ) : null}
    </div>
  );
}

const btnBase =
  "inline-flex items-center justify-center rounded-lg px-4 py-2 text-sm font-medium transition-colors disabled:pointer-events-none disabled:opacity-50";

export function Button({
  variant = "primary",
  className = "",
  type = "button",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "ghost" | "danger" }) {
  const variants = {
    primary: "bg-slate-900 text-white hover:bg-slate-800",
    secondary: "border border-slate-200 bg-white text-slate-700 hover:bg-slate-50",
    ghost: "text-slate-600 hover:bg-slate-100 hover:text-slate-900",
    danger: "bg-rose-600 text-white hover:bg-rose-700",
  };
  return <button type={type} className={`${btnBase} ${variants[variant]} ${className}`} {...props} />;
}

export function Field({ label, children, className = "" }: { label: string; children: ReactNode; className?: string }) {
  return (
    <label className={`block text-sm font-medium text-slate-700 ${className}`}>
      {label}
      {children}
    </label>
  );
}

export const inputClass =
  "mt-1.5 w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900 shadow-sm placeholder:text-slate-400 focus:border-slate-400 focus:outline-none focus:ring-2 focus:ring-slate-200/80";

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={inputClass} {...props} />;
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={inputClass} {...props} />;
}

export function TableWrap({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm">
      <table className="w-full min-w-[640px] text-left text-sm">{children}</table>
    </div>
  );
}

export function Th({ children }: { children?: ReactNode }) {
  return (
    <th className="bg-slate-50 px-4 py-3 text-xs font-semibold uppercase tracking-wide text-slate-500">{children}</th>
  );
}

export function Td({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <td className={`border-t border-slate-100 px-4 py-3 text-slate-700 ${className}`}>{children}</td>;
}

export function EmptyState({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-slate-200 bg-slate-50/80 px-6 py-12 text-center">
      <p className="text-base font-medium text-slate-800">{title}</p>
      {description ? <p className="mt-2 max-w-md text-sm text-slate-500">{description}</p> : null}
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  );
}

export function LoadingState({ message = "Loading…" }: { message?: string }) {
  return <p className="text-sm text-slate-500">{message}</p>;
}

export function Alert({
  variant,
  title,
  children,
  onRetry,
}: {
  variant: "error" | "success" | "warning" | "info";
  title?: string;
  children: ReactNode;
  onRetry?: () => void;
}) {
  const styles = {
    error: "border-rose-200 bg-rose-50 text-rose-800",
    success: "border-emerald-200 bg-emerald-50 text-emerald-800",
    warning: "border-amber-200 bg-amber-50 text-amber-900",
    info: "border-slate-200 bg-slate-50 text-slate-800",
  };
  return (
    <div className={`rounded-xl border p-4 text-sm ${styles[variant]}`} role={variant === "error" ? "alert" : undefined}>
      {title ? <p className="font-medium">{title}</p> : null}
      <div className={title ? "mt-1" : ""}>{children}</div>
      {onRetry ? (
        <button type="button" className="mt-3 font-medium underline underline-offset-2" onClick={onRetry}>
          Retry
        </button>
      ) : null}
    </div>
  );
}

export function Pagination({
  page,
  totalPages,
  onPrev,
  onNext,
}: {
  page: number;
  totalPages: number;
  onPrev: () => void;
  onNext: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 pt-2 text-sm text-slate-600">
      <Button variant="secondary" className="px-3 py-1.5" disabled={page <= 1} onClick={onPrev}>
        Previous
      </Button>
      <span className="tabular-nums">
        Page {page} of {totalPages}
      </span>
      <Button variant="secondary" className="px-3 py-1.5" disabled={page >= totalPages} onClick={onNext}>
        Next
      </Button>
    </div>
  );
}

export function AuthLayout({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) {
  return (
    <main className="flex flex-1 items-center justify-center px-4 py-12">
      <div className="w-full max-w-md">
        <div className="mb-8 text-center">
          <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">Ticket booking</p>
          <h1 className="mt-2 text-2xl font-semibold text-slate-900">{title}</h1>
          {subtitle ? <p className="mt-2 text-sm text-slate-500">{subtitle}</p> : null}
        </div>
        <Panel>{children}</Panel>
      </div>
    </main>
  );
}
