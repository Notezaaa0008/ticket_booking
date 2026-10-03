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

export type EventListItem = {
  id: string;
  title: string;
  venue: string;
  poster_url: string;
  next_showtime_at: string;
  min_price_satang: number;
};

export type EventListResponse = {
  items: EventListItem[];
  page: number;
  limit: number;
  total: number;
};

export type ListEventsParams = {
  q?: string;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export function listEvents(params: ListEventsParams = {}): Promise<EventListResponse> {
  const sp = new URLSearchParams();
  if (params.q) sp.set("q", params.q);
  if (params.from) sp.set("from", params.from);
  if (params.to) sp.set("to", params.to);
  if (params.page != null) sp.set("page", String(params.page));
  if (params.limit != null) sp.set("limit", String(params.limit));
  const qs = sp.toString();
  return apiFetch<EventListResponse>(`/api/v1/events${qs ? `?${qs}` : ""}`);
}

export type ShowtimeItem = {
  id: string;
  starts_at: string;
  status: string;
  available_seat_count: number;
};

export type EventDetail = {
  id: string;
  title: string;
  description: string;
  venue: string;
  poster_url: string;
  showtimes: ShowtimeItem[];
};

export function getEvent(id: string): Promise<EventDetail> {
  return apiFetch<EventDetail>(`/api/v1/events/${id}`);
}

export type SeatItem = {
  id: string;
  row_label: string;
  seat_number: number;
  zone: string;
  price_satang: number;
  status: "available" | "held" | "sold";
};

export type SeatSummary = {
  total: number;
  available: number;
  held: number;
  sold: number;
};

export type ShowtimeSeatsResponse = {
  seats: SeatItem[];
  summary: SeatSummary;
};

export function getShowtimeSeats(showtimeId: string): Promise<ShowtimeSeatsResponse> {
  return apiFetch<ShowtimeSeatsResponse>(`/api/v1/showtimes/${showtimeId}/seats`);
}
