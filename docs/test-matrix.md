# Test matrix

Integration tests in `backend/internal/handler`. Last race run: `go test ./... -race -count=1` passed (handler package included).

| Scenario | Test |
|---|---|
| Concurrent booking of the same seat | `TestBooking_ConcurrentSameSeat` |
| Partial multi-seat conflict; own holds released | `TestBooking_PartialHold_ReleasesOwnKeys` |
| Redis down; database unique index still prevents a double sale | `TestBooking_RedisDown_DBFallback` |
| Hold expiry job | `TestBooking_ExpiryJob` |
| Lazy expiry when a new booking needs those seats | `TestBooking_LazyExpiryOnNewBooking` |
| Cancel by owner, by another user, and when not pending | `TestBooking_Cancel` |
| Duplicate webhook, sequential | `TestPayment_DuplicateWebhookSequential` |
| Duplicate webhook, concurrent | `TestPayment_DuplicateWebhookConcurrent` |
| Invalid webhook signature | `TestPayment_InvalidSignatureRejected` |
| Amount mismatch | `TestPayment_AmountMismatchNeedsRefund` |
| Payment after expiry, seat rebooked | `TestPayment_AfterExpirySeatRebookedNeedsRefund` |
| Retry after a failed payment | `TestPayment_GatewayFailureThenRetry` |
| Refund success | `TestAdmin_RefundSuccessFutureShowtimeReleasesSeats` |
| Refund provider failure, then a new request | `TestAdmin_RefundProviderFailureThenRetry` |
| Duplicate and concurrent refund process | `TestAdmin_RefundProcessTwiceAndConcurrently` |
| Double check-in, including a concurrent race | `TestAdmin_CheckIn` |
| Every admin route returns 403 for a normal user | `TestAdmin_EveryRouteRequiresAdmin` |
| Expired JWT | `TestAuth_TokenChecks` |
