#!/usr/bin/env node
/**
 * Phase 3 browser checks (Playwright). Requires:
 *   backend on :8080, frontend on :3000 (use localhost for the UI — CORS)
 *   npm install playwright (in frontend or repo root)
 * Run: node scripts/phase3-browser-e2e.mjs
 */
import { chromium } from 'playwright';
import { execSync } from 'node:child_process';

const API = 'http://127.0.0.1:8080';
const WEB = 'http://localhost:3000';
const SHOW = '55555555-0000-0000-0000-000000000001';
const results = [];

function ok(name, pass, detail = '') {
  results.push({ name, pass, detail });
  console.log(`${pass ? 'PASS' : 'FAIL'} ${name}${detail ? ': ' + detail : ''}`);
}

async function apiRegister(email, password, name) {
  const res = await fetch(`${API}/api/v1/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, name }),
  });
  if (!res.ok) throw new Error(`register ${res.status}`);
}

async function apiLogin(email, password) {
  const res = await fetch(`${API}/api/v1/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) throw new Error(`login ${res.status}`);
  const body = await res.json();
  return body.token;
}

async function apiBook(token, showtimeId, seatIds) {
  const res = await fetch(`${API}/api/v1/bookings`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ showtime_id: showtimeId, seat_ids: seatIds }),
  });
  const body = await res.json().catch(() => ({}));
  return { status: res.status, body };
}

async function apiCancel(token, bookingId) {
  const res = await fetch(`${API}/api/v1/bookings/${bookingId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.status;
}

/** Seat map "available" can lag DB; try until POST booking succeeds. */
async function bookFirstAvailableSeat(token) {
  const seatsJson = await fetch(`${API}/api/v1/showtimes/${SHOW}/seats`).then((r) => r.json());
  for (const s of seatsJson.seats.filter((x) => x.status === 'available')) {
    const res = await apiBook(token, SHOW, [s.id]);
    if (res.status === 201) return { seat: s, booking: res.body };
  }
  return null;
}

async function login(page, email, password) {
  await page.goto(`${WEB}/login`);
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Log in' }).click();
  await page.waitForFunction(() => !window.location.pathname.startsWith('/login'), null, { timeout: 20000 });
}

function expireBookingInDB(bookingId, userId, showtimeId) {
  const repoRoot = new URL('..', import.meta.url).pathname;
  const sql =
    `UPDATE bookings SET status = 'EXPIRED' WHERE id = '${bookingId}'; ` +
    `UPDATE booking_items SET active = false WHERE booking_id = '${bookingId}'; ` +
    `INSERT INTO booking_events (booking_id, user_id, showtime_id, event_type, outcome, reason_code, from_status, to_status, actor_type, metadata) ` +
    `VALUES ('${bookingId}', '${userId}', '${showtimeId}', 'BOOKING_EXPIRED', 'SUCCESS', 'HOLD_EXPIRED', 'PENDING', 'EXPIRED', 'SYSTEM', '{}');`;
  execSync(`docker compose exec -T postgres psql -U ticket -d ticket_booking -v ON_ERROR_STOP=1 -c ${JSON.stringify(sql)}`, {
    stdio: 'pipe',
    cwd: repoRoot,
  });
}

const browser = await chromium.launch({ headless: true });
try {
  const anon = await browser.newContext();
  const p0 = await anon.newPage();
  await p0.goto(`${WEB}/bookings`);
  await p0.waitForFunction(() => location.pathname.startsWith('/login'), null, { timeout: 20000 });
  ok('P3-30 redirect /bookings -> /login', p0.url().includes('/login'));

  const email = `e2e${Date.now()}@example.com`;
  const ctx1 = await browser.newContext();
  const p1 = await ctx1.newPage();
  await p1.goto(`${WEB}/register`);
  await p1.getByLabel('Name').fill('E2E User');
  await p1.getByLabel('Email').fill(email);
  await p1.getByLabel(/^Password/).fill('password123');
  await p1.getByRole('button', { name: 'Register' }).click();
  await p1.waitForFunction(() => !location.pathname.startsWith('/register'), null, { timeout: 20000 });
  await p1.getByRole('button', { name: 'Log out' }).click();
  await p1.waitForFunction(() => location.pathname.startsWith('/login'), null, { timeout: 10000 });
  ok('P3-30 register, logout', true);

  const buyerEmail = `buyer${Date.now()}@example.com`;
  await apiRegister(buyerEmail, 'password123', 'Buyer');

  function pickTwoSeats(seats, skipFirst) {
    const avail = seats
      .filter((s) => s.status === 'available')
      .sort((a, b) => a.row_label.localeCompare(b.row_label) || a.seat_number - b.seat_number);
    const d1 = avail.find((s) => !skipFirst.has(s.id));
    if (!d1) return null;
    const d2 = avail.find((s) => s.id !== d1.id && !skipFirst.has(s.id));
    if (!d2) return null;
    return { d1, d2 };
  }

  const ctxBuyer = await browser.newContext();
  const buyer = await ctxBuyer.newPage();
  await login(buyer, buyerEmail, 'password123');

  // P3-31: buyer selects two seats, holder takes the first via API, buyer book fails and map refreshes.
  let d1;
  let d2;
  let labelD1;
  let labelD2;
  let holdOk = false;
  const skipFirst = new Set();
  for (let attempt = 0; attempt < 12; attempt++) {
    const seatsJson = await fetch(`${API}/api/v1/showtimes/${SHOW}/seats`).then((r) => r.json());
    const pair = pickTwoSeats(seatsJson.seats, skipFirst);
    if (!pair) throw new Error('need two available seats');
    d1 = pair.d1;
    d2 = pair.d2;
    labelD1 = `Row ${d1.row_label} seat ${d1.seat_number}`;
    labelD2 = `Row ${d2.row_label} seat ${d2.seat_number}`;
    await buyer.goto(`${WEB}/showtimes/${SHOW}?t=${Date.now()}`);
    await buyer.getByRole('button', { name: labelD1, exact: true }).click();
    await buyer.getByRole('button', { name: labelD2, exact: true }).click();
    const attemptHolder = `holder${Date.now()}_${attempt}@example.com`;
    await apiRegister(attemptHolder, 'password123', 'Holder');
    const attemptTok = await apiLogin(attemptHolder, 'password123');
    const holdRes = await apiBook(attemptTok, SHOW, [d1.id]);
    if (holdRes.status === 201) {
      holdOk = true;
      break;
    }
    skipFirst.add(d1.id);
    await buyer.getByRole('button', { name: labelD2, exact: true }).click().catch(() => {});
    await buyer.getByRole('button', { name: labelD1, exact: true }).click().catch(() => {});
  }
  ok('P3-31 setup hold D1', holdOk, holdOk ? '201' : 'no seat');
  if (!holdOk) throw new Error('P3-31 setup failed');
  await buyer.getByRole('button', { name: /Book selected seats/ }).click();
  const conflictMsg = buyer.getByText(/just taken|no longer available|Sorry/i);
  await conflictMsg.waitFor({ timeout: 15000 });
  const alertText = await conflictMsg.textContent();
  ok('P3-31 conflict message', /Sorry|taken|unavailable|just/i.test(alertText || ''), alertText || '(empty)');
  const d2Selected = await buyer
    .getByRole('button', { name: labelD2, exact: true })
    .evaluate((el) => el.className.includes('bg-blue-600'));
  ok('P3-31 keeps other seat selected', d2Selected);
  await buyer
    .getByRole('button', { name: labelD1, exact: true })
    .waitFor({ state: 'visible', timeout: 15000 });
  await buyer.waitForFunction(
    (label) => {
      const btn = [...document.querySelectorAll('button[aria-label]')].find((b) => b.getAttribute('aria-label') === label);
      return btn?.className.includes('bg-yellow-100');
    },
    labelD1,
    { timeout: 15000 },
  );
  ok('P3-31 seat map refresh shows held', true);

  // P3-33 race (same user, two contexts) on row E
  const raceEmail = `race${Date.now()}@example.com`;
  await apiRegister(raceEmail, 'password123', 'Race');
  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  const pageA = await ctxA.newPage();
  const pageB = await ctxB.newPage();
  await login(pageA, raceEmail, 'password123');
  await login(pageB, raceEmail, 'password123');
  async function bookRaceOutcome(page) {
    await page.getByRole('button', { name: /Book selected seats/ }).click();
    try {
      await page.waitForURL(/\/bookings\//, { timeout: 20000 });
      return 'won';
    } catch {
      return 'lost';
    }
  }

  const probeTok = await apiLogin(raceEmail, 'password123');
  const probe = await bookFirstAvailableSeat(probeTok);
  if (probe) {
    await apiCancel(probeTok, probe.booking.id);
  }

  let winner = null;
  for (let raceTry = 0; raceTry < 5; raceTry++) {
    const raceSeats = await fetch(`${API}/api/v1/showtimes/${SHOW}/seats`).then((r) => r.json());
    const target =
      probe?.seat && raceSeats.seats.find((s) => s.id === probe.seat.id && s.status === 'available')
        ? probe.seat
        : raceSeats.seats.find((s) => s.status === 'available');
    if (!target) break;
    const seatLabel = `Row ${target.row_label} seat ${target.seat_number}`;
    await pageA.goto(`${WEB}/showtimes/${SHOW}?r=${raceTry}`);
    await pageB.goto(`${WEB}/showtimes/${SHOW}?r=${raceTry}`);
    const seatA = pageA.getByRole('button', { name: seatLabel, exact: true });
    const seatB = pageB.getByRole('button', { name: seatLabel, exact: true });
    if (await seatA.isDisabled().catch(() => true)) continue;
    await seatA.click();
    await seatB.click();
    const [outA, outB] = await Promise.all([bookRaceOutcome(pageA), bookRaceOutcome(pageB)]);
    const wins = [outA, outB].filter((x) => x === 'won').length;
    if (wins === 1) {
      winner = outA === 'won' ? pageA : pageB;
      break;
    }
  }
  ok('P3-33 dual window same seat', winner !== null, winner ? '' : 'no single winner');

  const panelEmail = `panel${Date.now()}@example.com`;
  await apiRegister(panelEmail, 'password123', 'Panel');
  const panelTok = await apiLogin(panelEmail, 'password123');
  const panelCreated = await bookFirstAvailableSeat(panelTok);
  if (!panelCreated) throw new Error('pending panel setup failed');
  const panelBook = { status: 201, body: panelCreated.booking };
  const panelCtx = await browser.newContext();
  winner = await panelCtx.newPage();
  await login(winner, panelEmail, 'password123');
  await winner.goto(`${WEB}/bookings/${panelBook.body.id}`);
  await winner.waitForSelector('text=Seats are held for you', { timeout: 10000 });
  ok('P3-32 PENDING panel', /Seats are held/i.test(await winner.content()));

  const t1 = await winner.locator('[aria-live="polite"]').textContent();
  await winner.waitForTimeout(2000);
  const t2 = await winner.locator('[aria-live="polite"]').textContent();
  const toSec = (t) => parseInt((t || '0:0').split(':').reduce((a, b) => a * 60 + parseInt(b, 10), 0), 10);
  ok('P3-19 countdown ticks down', toSec(t2) < toSec(t1), `${t1} -> ${t2}`);

  const pendingUrl = winner.url();

  // P3-32 CANCELLED
  winner.once('dialog', (d) => d.accept());
  await winner.getByRole('button', { name: 'Cancel booking' }).click();
  await winner.waitForSelector('text=Booking cancelled', { timeout: 15000 });
  ok(
    'P3-32 CANCELLED panel',
    /Booking cancelled/i.test((await winner.locator('body').innerText()) || ''),
  );

  // P3-32 EXPIRED (API booking + DB expire for deterministic UI)
  const expEmail = `exp${Date.now()}@example.com`;
  await apiRegister(expEmail, 'password123', 'Expiry');
  const expTok = await apiLogin(expEmail, 'password123');
  const expCreated = await bookFirstAvailableSeat(expTok);
  ok('P3-32 setup booking for expiry', !!expCreated, expCreated ? '201' : 'none');
  if (!expCreated) throw new Error('expiry setup failed');
  const expId = expCreated.booking.id;
  const me = await fetch(`${API}/api/v1/me`, { headers: { Authorization: `Bearer ${expTok}` } }).then((r) => r.json());
  expireBookingInDB(expId, me.user.id, SHOW);

  await login(buyer, expEmail, 'password123');

  await buyer.goto(`${WEB}/bookings/${expId}`);
  await buyer.waitForSelector('text=Booking expired', { timeout: 15000 });
  ok(
    'P3-32 EXPIRED panel',
    /Booking expired|HOLD_EXPIRED/i.test((await buyer.locator('body').innerText()) || ''),
  );

  const ctx3 = await browser.newContext();
  const p3 = await ctx3.newPage();
  await login(p3, panelEmail, 'password123');
  await p3.goto(pendingUrl);
  await p3.waitForSelector('text=Booking cancelled', { timeout: 10000 });
  ok(
    'P3-19 reload cancelled booking',
    /Booking cancelled/i.test((await p3.locator('body').innerText()) || ''),
  );
} finally {
  await browser.close();
}
process.exit(results.some((r) => !r.pass) ? 1 : 0);
