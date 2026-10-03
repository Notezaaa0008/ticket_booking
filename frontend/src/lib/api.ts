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

// ---------- admin ----------

export type Page<T> = { items: T[]; page: number; limit: number; total: number };

export type AdminShowtime = {
  id: string;
  event_id: string;
  starts_at: string;
  status: "on_sale" | "closed" | "cancelled";
  seat_count: number;
  booking_count: number;
};

export type AdminEvent = {
  id: string;
  title: string;
  description: string;
  venue: string;
  poster_url: string;
  status: "draft" | "published" | "archived";
  created_at: string;
  showtimes: AdminShowtime[];
};

export type EventInput = { title?: string; description?: string; venue?: string; poster_url?: string; status?: string };

export async function adminListEvents(): Promise<AdminEvent[]> {
  return (await apiFetch<{ items: AdminEvent[] }>("/api/v1/admin/events")).items;
}

export function adminCreateEvent(input: EventInput): Promise<AdminEvent> {
  return apiFetch<AdminEvent>("/api/v1/admin/events", { method: "POST", body: JSON.stringify(input) });
}

export function adminDeleteEvent(id: string): Promise<{ result: "deleted" | "archived" }> {
  return apiFetch<{ result: "deleted" | "archived" }>(`/api/v1/admin/events/${id}`, { method: "DELETE" });
}

export type ShowtimeInput = {
  starts_at: string;
  rows: number;
  seats_per_row: number;
  price_satang: number;
  price_satang_by_row?: Record<string, number>;
};

export function adminCreateShowtime(eventId: string, input: ShowtimeInput): Promise<AdminShowtime> {
  return apiFetch<AdminShowtime>(`/api/v1/admin/events/${eventId}/showtimes`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function adminUpdateShowtime(
  id: string,
  input: { status?: AdminShowtime["status"]; starts_at?: string },
): Promise<AdminShowtime> {
  return apiFetch<AdminShowtime>(`/api/v1/admin/showtimes/${id}`, { method: "PUT", body: JSON.stringify(input) });
}

export type AdminBooking = {
  id: string;
  user_email: string;
  showtime_id: string;
  starts_at: string;
  event_title: string;
  seats: string[];
  status: BookingStatus;
  total_satang: number;
  latest_payment_status: PaymentStatus | "";
  created_at: string;
  expires_at: string;
};

export type AdminBookingListParams = {
  status?: string;
  user_id?: string;
  email?: string;
  showtime_id?: string;
  page?: number;
};

export function adminListBookings(params: AdminBookingListParams = {}): Promise<Page<AdminBooking>> {
  const sp = new URLSearchParams({ page: String(params.page ?? 1) });
  if (params.status) sp.set("status", params.status);
  if (params.user_id) sp.set("user_id", params.user_id);
  if (params.email) sp.set("email", params.email);
  if (params.showtime_id) sp.set("showtime_id", params.showtime_id);
  return apiFetch<Page<AdminBooking>>(`/api/v1/admin/bookings?${sp.toString()}`);
}

export type AdminPayment = Payment & {
  provider_ref: string;
  user_email?: string;
  booking_status?: BookingStatus;
  event_title?: string;
};

export function adminListPayments(status: string, page: number): Promise<Page<AdminPayment>> {
  const sp = new URLSearchParams({ page: String(page) });
  if (status) sp.set("status", status);
  return apiFetch<Page<AdminPayment>>(`/api/v1/admin/payments?${sp.toString()}`);
}

export type RefundStatus = "REQUESTED" | "COMPLETED" | "FAILED" | "REJECTED";

export type Refund = {
  id: string;
  payment_id: string;
  booking_id: string;
  amount_satang: number;
  status: RefundStatus;
  reason_code: string;
  note: string;
  provider_ref?: string;
  failure_code?: string;
  created_at: string;
  completed_at: string | null;
};

export type Outcome = "SUCCESS" | "FAILURE" | "IGNORED";

export type TimelineEvent = {
  source: "booking" | "payment";
  event_type: string;
  outcome: Outcome;
  reason_code: string;
  actor_type: string;
  from_status: string;
  to_status: string;
  created_at: string;
};

export type BookingTimeline = {
  booking: AdminBooking;
  payments: AdminPayment[];
  refunds: Refund[];
  events: TimelineEvent[];
};

export function adminBookingTimeline(id: string): Promise<BookingTimeline> {
  return apiFetch<BookingTimeline>(`/api/v1/admin/bookings/${id}/timeline`);
}

/** The same idempotency key returns the same refund, so a double click never opens two. */
export function adminRequestRefund(
  paymentId: string,
  reasonCode: string,
  note: string,
  idempotencyKey: string,
): Promise<Refund> {
  return apiFetch<Refund>(`/api/v1/admin/payments/${paymentId}/refunds`, {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ reason_code: reasonCode, note }),
  });
}

export function adminProcessRefund(refundId: string): Promise<Refund> {
  return apiFetch<Refund>(`/api/v1/admin/refunds/${refundId}/process`, { method: "POST" });
}

export function adminListRefunds(status: string, page: number): Promise<Page<Refund>> {
  const sp = new URLSearchParams({ page: String(page) });
  if (status) sp.set("status", status);
  return apiFetch<Page<Refund>>(`/api/v1/admin/refunds?${sp.toString()}`);
}

export type CheckInResult = { result: "CHECKED_IN"; ticket: Ticket };

export function adminCheckIn(code: string): Promise<CheckInResult> {
  return apiFetch<CheckInResult>(`/api/v1/admin/tickets/${encodeURIComponent(code)}/check-in`, { method: "POST" });
}

export type AdminStats = {
  revenue_satang: number;
  tickets_sold: number;
  bookings_by_status: Record<string, number>;
  payments_by_status: Record<string, number>;
  needs_refund_count: number;
  bookings_today: number;
};

export function adminStats(): Promise<AdminStats> {
  return apiFetch<AdminStats>("/api/v1/admin/stats");
}
