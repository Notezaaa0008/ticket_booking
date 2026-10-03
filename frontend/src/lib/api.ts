const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export class ApiError extends Error {
  status: number;
  code: string;
  /** Seat ids named by the server in a SEAT_UNAVAILABLE conflict. */
  seatIds: string[];
  constructor(status: number, code: string, message: string, seatIds: string[] = []) {
    super(message);
    this.status = status;
    this.code = code;
    this.seatIds = seatIds;
  }
}

// The token lives in localStorage (simple, but readable by any XSS; see README note).
const TOKEN_KEY = "tb_token";

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}

const AUTH_TOKEN_EVENT = "tb-auth-token";

/** Subscribe to login/logout (same-tab and other tabs). */
export function subscribeAuthToken(onStoreChange: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  const onChange = () => onStoreChange();
  window.addEventListener(AUTH_TOKEN_EVENT, onChange);
  window.addEventListener("storage", onChange);
  return () => {
    window.removeEventListener(AUTH_TOKEN_EVENT, onChange);
    window.removeEventListener("storage", onChange);
  };
}

export function hasAuthToken(): boolean {
  return getToken() !== null;
}

export function setToken(token: string | null): void {
  if (typeof window === "undefined") return;
  if (token) window.localStorage.setItem(TOKEN_KEY, token);
  else window.localStorage.removeItem(TOKEN_KEY);
  window.dispatchEvent(new Event(AUTH_TOKEN_EVENT));
}

type FetchInit = Omit<RequestInit, "headers"> & { headers?: Record<string, string> };

/** Calls the backend. Throws ApiError built from the standard {"error":{"code","message"}} body. */
export async function apiFetch<T>(path: string, init: FetchInit = {}): Promise<T> {
  const token = getToken();
  const res = await fetch(`${BASE_URL}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init.headers ?? {}),
    },
  });
  const text = await res.text();
  const data: unknown = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string; seat_ids?: string[] } } | null)?.error;
    throw new ApiError(res.status, err?.code ?? "UNKNOWN", err?.message ?? res.statusText, err?.seat_ids ?? []);
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

// ---------- auth ----------

export type User = { id: string; email: string; name: string; role: "user" | "admin" };

export function register(email: string, password: string, name: string): Promise<{ user: User }> {
  return apiFetch<{ user: User }>("/api/v1/auth/register", {
    method: "POST",
    body: JSON.stringify({ email, password, name }),
  });
}

export function login(email: string, password: string): Promise<{ token: string; user: User }> {
  return apiFetch<{ token: string; user: User }>("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

export async function getMe(): Promise<User> {
  return (await apiFetch<{ user: User }>("/api/v1/me")).user;
}

// ---------- bookings ----------

export type BookingStatus = "PENDING" | "PAID" | "EXPIRED" | "CANCELLED" | "REFUNDED";

export type BookingItem = {
  seat_id: string;
  row_label: string;
  seat_number: number;
  zone: string;
  price_satang: number;
};

export type Booking = {
  id: string;
  showtime_id: string;
  event_title: string;
  venue: string;
  starts_at: string;
  status: BookingStatus;
  status_reason?: string;
  total_satang: number;
  expires_at: string;
  seconds_remaining: number;
  created_at: string;
  items: BookingItem[];
  /** Latest payment; only on GET /bookings/:id. */
  payment?: Payment;
  /** Only when PAID, only on GET /bookings/:id. */
  tickets?: Ticket[];
};

/** Sends only ids: prices and totals are always computed by the server. */
export function createBooking(showtimeId: string, seatIds: string[]): Promise<Booking> {
  return apiFetch<Booking>("/api/v1/bookings", {
    method: "POST",
    body: JSON.stringify({ showtime_id: showtimeId, seat_ids: seatIds }),
  });
}

export async function listBookings(): Promise<Booking[]> {
  return (await apiFetch<{ items: Booking[] }>("/api/v1/bookings")).items;
}

export function getBooking(id: string): Promise<Booking> {
  return apiFetch<Booking>(`/api/v1/bookings/${id}`);
}

export function cancelBooking(id: string): Promise<Booking> {
  return apiFetch<Booking>(`/api/v1/bookings/${id}`, { method: "DELETE" });
}

// ---------- payments and tickets ----------

export type PaymentStatus = "PENDING" | "SUCCEEDED" | "FAILED" | "NEEDS_REFUND" | "REFUNDED";

export type Payment = {
  id: string;
  booking_id: string;
  status: PaymentStatus;
  amount_satang: number;
  failure_code?: string;
  failure_message?: string;
  paid_at: string | null;
  created_at: string;
  pay_url: string;
};

/** The same idempotency key returns the same payment, so a double click never creates two. */
export function createPayment(bookingId: string, idempotencyKey: string): Promise<Payment> {
  return apiFetch<Payment>(`/api/v1/bookings/${bookingId}/payments`, {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
  });
}

export function getPayment(id: string): Promise<Payment> {
  return apiFetch<Payment>(`/api/v1/payments/${id}`);
}

export type MockPayResult = { webhook_status: number };

/** Dev-only mock gateway: the backend signs and delivers the webhook itself. */
export function mockPay(paymentId: string, result: "success" | "fail"): Promise<MockPayResult> {
  return apiFetch<MockPayResult>(`/api/v1/mock-gateway/${paymentId}/pay`, {
    method: "POST",
    body: JSON.stringify({ result }),
  });
}

export type TicketStatus = "VALID" | "USED" | "VOID";

export type Ticket = {
  code: string;
  status: TicketStatus;
  booking_id: string;
  event_title: string;
  venue: string;
  starts_at: string;
  seat_label: string;
  zone: string;
  issued_at: string;
};

export async function listTickets(): Promise<Ticket[]> {
  return (await apiFetch<{ items: Ticket[] }>("/api/v1/tickets")).items;
}
