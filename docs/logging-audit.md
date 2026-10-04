# Logging audit

A booking, payment, refund, or ticket state change and its terminal event commit in one transaction. A rolled-back failure is written afterward on the plain connection. Webhook rejections that never change a payment commit `WEBHOOK_RECEIVED` and `WEBHOOK_REJECTED` in one transaction. Catalog and auth methods do not write these tables.

## Booking (`internal/service/booking.go`)

| Method | State | Event | reason_code | Test |
|---|---|---|---|---|
| `Create` success | booking `PENDING`, items active | `BOOKING_CREATED` SUCCESS | | `TestBooking_CreateViewsAndAudit` |
| `Create` / `RejectInvalid` failure | none | `BOOKING_CREATE_FAILED` FAILURE | `SEAT_UNAVAILABLE`, `VALIDATION_FAILED`, `TOO_MANY_SEATS`, `SHOWTIME_NOT_ON_SALE`, `SEAT_NOT_IN_SHOWTIME` | `TestBooking_ConcurrentSameSeat`, `TestBooking_PartialHold_ReleasesOwnKeys`, `TestBooking_ValidationFailuresAreAudited`, `TestBooking_SixSeatsAllowedSevenRejected`, `TestBooking_OnePendingPerShowtime` |
| `Cancel` success | `PENDING` → `CANCELLED`, items inactive | `BOOKING_CANCELLED` SUCCESS | `USER_CANCELLED` | `TestBooking_Cancel` |
| `Cancel` rejected | none | `BOOKING_CANCEL_REJECTED` FAILURE | `BOOKING_NOT_FOUND`, `BOOKING_NOT_PENDING` | `TestBooking_Cancel` |
| `expireLocked` via `ExpireDue` or lazy expiry inside `Create` | `PENDING` → `EXPIRED`, items inactive | `BOOKING_EXPIRED` SUCCESS, actor `SYSTEM` | `HOLD_EXPIRED` | `TestBooking_ExpiryJob`, `TestBooking_LazyExpiryOnNewBooking` |
| `Get`, `List` | none | none | `BOOKING_NOT_FOUND` on get | `TestBooking_CreateViewsAndAudit` |

Redis hold failure does not fail the booking. `TestBooking_RedisDown_DBFallback` checks `hold_mode=db_fallback` on `BOOKING_CREATED`.

## Payment (`internal/service/payment.go`)

| Method | State | Event | reason_code | Test |
|---|---|---|---|---|
| `Create` success | payment `PENDING` | `PAYMENT_CREATED` SUCCESS | | `TestPayment_SuccessIssuesTickets` |
| `Create` failure | none | `PAYMENT_CREATE_FAILED` FAILURE | `BOOKING_NOT_PAYABLE`, `BOOKING_EXPIRED`, `BOOKING_NOT_FOUND`, `VALIDATION_FAILED` | `TestPayment_CreateRules`, `TestPayment_CancelledBookingNotPayable` |
| `HandleWebhook` bad signature | none | `WEBHOOK_RECEIVED` then `WEBHOOK_REJECTED` FAILURE | `INVALID_SIGNATURE` | `TestPayment_InvalidSignatureRejected` |
| `HandleWebhook` malformed body | none | `WEBHOOK_RECEIVED` then `WEBHOOK_REJECTED` FAILURE | `MALFORMED_PAYLOAD` | `TestPayment_MalformedAndUnknownWebhook` |
| `HandleWebhook` payment lookup error | none | `WEBHOOK_RECEIVED` then `WEBHOOK_REJECTED` FAILURE | `INTERNAL_ERROR` | `TestPayment_WebhookLookupFailureIsAudited` |
| `HandleWebhook` unknown payment | none | `WEBHOOK_RECEIVED` then `WEBHOOK_REJECTED` FAILURE | `UNKNOWN_PAYMENT` | `TestPayment_MalformedAndUnknownWebhook` |
| `HandleWebhook` succeeded | payment `SUCCEEDED`, booking `PAID`, tickets inserted | `PAYMENT_SUCCEEDED`, `BOOKING_PAID`, `TICKETS_ISSUED` | | `TestPayment_SuccessIssuesTickets` |
| `HandleWebhook` gateway failed | payment `FAILED`; booking stays `PENDING` | `PAYMENT_FAILED` FAILURE | `GATEWAY_DECLINED` | `TestPayment_GatewayFailureThenRetry` |
| `HandleWebhook` duplicate | none | `PAYMENT_DUPLICATE_IGNORED` IGNORED | `DUPLICATE_EVENT` | `TestPayment_DuplicateWebhookSequential`, `TestPayment_DuplicateWebhookConcurrent` |
| `HandleWebhook` amount mismatch | payment `NEEDS_REFUND` | `PAYMENT_AMOUNT_MISMATCH` FAILURE | `AMOUNT_MISMATCH` | `TestPayment_AmountMismatchNeedsRefund` |
| `HandleWebhook` after expiry, including when the seat was rebooked | payment `NEEDS_REFUND` | `PAYMENT_AFTER_EXPIRY` FAILURE | `BOOKING_EXPIRED` | `TestPayment_AfterExpirySeatRebookedNeedsRefund` |
| `HandleWebhook` seats lost while the booking is not `EXPIRED` (cancelled, then rebooked) | payment `NEEDS_REFUND` | `PAYMENT_AFTER_EXPIRY` FAILURE | `SEAT_LOST` | `TestPayment_SeatLostAfterCancelAndRebookNeedsRefund` |
| `HandleWebhook` processing error after receive | rolled back | `WEBHOOK_REJECTED` FAILURE | `INTERNAL_ERROR` | `TestPayment_WebhookInternalError500` |
| `Get`, `ListTickets`, `GetTicket` | none | none | `NOT_FOUND` | `TestPayment_OwnershipAndDisabledGateway` |

## Refund (`internal/service/refund.go`)

| Method | State | Event | reason_code | Test |
|---|---|---|---|---|
| `RequestRefund` success | refund row `REQUESTED`; payment unchanged | `REFUND_REQUESTED` SUCCESS | | `TestAdmin_RefundEligibilityMatrix` |
| `RequestRefund` rejected | none | `REFUND_REJECTED` FAILURE | `REFUND_NOT_ELIGIBLE`, `TICKET_ALREADY_USED`, `REFUND_ALREADY_EXISTS`, `AMOUNT_EXCEEDS_PAYMENT`, `VALIDATION_FAILED`, `UNKNOWN_PAYMENT` | `TestAdmin_RefundEligibilityMatrix`, `TestAdmin_RefundAmountExceedsPayment` |
| `ProcessRefund` success | refund `COMPLETED`, payment `REFUNDED`, tickets `VOID`, booking `REFUNDED` if it was `PAID` | `REFUND_COMPLETED` and `BOOKING_REFUNDED` | | `TestAdmin_RefundSuccessFutureShowtimeReleasesSeats`, `TestAdmin_RefundSuccessStartedShowtimeKeepsSeats` |
| `ProcessRefund` provider failure | refund `FAILED`; payment and booking unchanged | `REFUND_FAILED` FAILURE | `PROVIDER_FAILED` | `TestAdmin_RefundProviderFailureThenRetry` |
| `ProcessRefund` missing refund | none | `REFUND_REJECTED` FAILURE | `NOT_FOUND` | `TestAdmin_ProcessRefundMissingAndUnexpectedRef` |
| `ProcessRefund` unexpected `provider_ref` | refund stays `REQUESTED` | `REFUND_FAILED` FAILURE | `INTERNAL_ERROR` | `TestAdmin_ProcessRefundMissingAndUnexpectedRef` |
| `ProcessRefund` replay of a finished refund | none | none (returns the existing row) | | `TestAdmin_RefundProcessTwiceAndConcurrently` |

## Check-in (`internal/service/admin.go`)

| Method | State | Event | reason_code | Test |
|---|---|---|---|---|
| `CheckIn` success | ticket `VALID` → `USED` | `TICKET_CHECKED_IN` SUCCESS | | `TestAdmin_CheckIn` |
| `CheckIn` rejected | none | `TICKET_CHECK_IN_REJECTED` FAILURE | `TICKET_ALREADY_USED`, `TICKET_VOID`, `NOT_FOUND` | `TestAdmin_CheckIn` |

## No booking or payment event

| Method | What it changes or returns | Test |
|---|---|---|
| `AuthService.Register`, `Login`, `Me` | user row or `EMAIL_TAKEN` / `INVALID_CREDENTIALS` / `UNAUTHENTICATED` | `TestAuth_RegisterAndLogin`, `TestAuth_LoginFailuresAreIndistinguishable`, `TestAuth_TokenChecks` |
| `CatalogService.ListEvents`, `GetEvent`, `GetShowtimeSeats` | read; `VALIDATION_FAILED` / `NOT_FOUND` | `TestCatalogListSearchAndValidation`, `TestCatalogExcludesUnpublishedAndPast` |
| `AdminService` event and showtime create/update/delete | catalog rows only | `TestAdmin_EventsAndShowtimes` |
| `AdminService.ListBookings`, `ListPayments`, `ListRefunds`, `Timeline`, `Stats` | read | `TestAdmin_StatsListsAndTimeline` |
