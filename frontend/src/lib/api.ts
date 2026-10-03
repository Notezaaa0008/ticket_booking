const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

type FetchInit = Omit<RequestInit, "headers"> & { headers?: Record<string, string> };

/** Calls the backend. Throws ApiError built from the standard {"error":{"code","message"}} body. */
export async function apiFetch<T>(path: string, init: FetchInit = {}): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init.headers ?? {}) },
  });
  const text = await res.text();
  const data: unknown = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string } } | null)?.error;
    throw new ApiError(res.status, err?.code ?? "UNKNOWN", err?.message ?? res.statusText);
  }
  return data as T;
}

export type Health = { status: string; db: string; redis: string };

/** /healthz returns JSON for both 200 and 503, so it is read directly. */
export async function getHealth(): Promise<Health> {
  const res = await fetch(`${BASE_URL}/healthz`, { cache: "no-store" });
  return (await res.json()) as Health;
}
