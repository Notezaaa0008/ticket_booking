// Package domain holds the constants shared by services, handlers and the audit log.
// Values here MUST match the CHECK constraints in migrations/0001_init.up.sql.
// Adding or renaming a value needs human approval (see AGENTS.md section 10).
package domain

import "net/http"

// ---- statuses ----

type BookingStatus string

const (
	BookingPending   BookingStatus = "PENDING"
	BookingPaid      BookingStatus = "PAID"
	BookingExpired   BookingStatus = "EXPIRED"
	BookingCancelled BookingStatus = "CANCELLED"
	BookingRefunded  BookingStatus = "REFUNDED"
)

type PaymentStatus string

const (
	PaymentPending     PaymentStatus = "PENDING"
	PaymentSucceeded   PaymentStatus = "SUCCEEDED"
	PaymentFailed      PaymentStatus = "FAILED"
	PaymentNeedsRefund PaymentStatus = "NEEDS_REFUND"
	PaymentRefunded    PaymentStatus = "REFUNDED"
)

type RefundStatus string

const (
	RefundRequested RefundStatus = "REQUESTED"
	RefundCompleted RefundStatus = "COMPLETED"
	RefundFailed    RefundStatus = "FAILED"
	RefundRejected  RefundStatus = "REJECTED"
)

type TicketStatus string

const (
	TicketValid TicketStatus = "VALID"
	TicketUsed  TicketStatus = "USED"
	TicketVoid  TicketStatus = "VOID"
)

// ---- audit log values ----

type Outcome string

const (
	OutcomeSuccess Outcome = "SUCCESS"
	OutcomeFailure Outcome = "FAILURE"
	OutcomeIgnored Outcome = "IGNORED"
)

func (o Outcome) Valid() bool {
	return o == OutcomeSuccess || o == OutcomeFailure || o == OutcomeIgnored
}

type ActorType string

const (
	ActorUser    ActorType = "USER"
	ActorAdmin   ActorType = "ADMIN"
	ActorSystem  ActorType = "SYSTEM"
	ActorGateway ActorType = "GATEWAY"
)

func (a ActorType) Valid() bool {
	return a == ActorUser || a == ActorAdmin || a == ActorSystem || a == ActorGateway
}

type BookingEventType string

const (
	EvBookingCreated        BookingEventType = "BOOKING_CREATED"
	EvBookingCreateFailed   BookingEventType = "BOOKING_CREATE_FAILED"
	EvBookingPaid           BookingEventType = "BOOKING_PAID"
	EvBookingExpired        BookingEventType = "BOOKING_EXPIRED"
	EvBookingCancelled      BookingEventType = "BOOKING_CANCELLED"
	EvBookingCancelRejected BookingEventType = "BOOKING_CANCEL_REJECTED"
	EvBookingRefunded       BookingEventType = "BOOKING_REFUNDED"
	EvTicketsIssued         BookingEventType = "TICKETS_ISSUED"
)

var AllBookingEventTypes = []BookingEventType{
	EvBookingCreated, EvBookingCreateFailed, EvBookingPaid, EvBookingExpired,
	EvBookingCancelled, EvBookingCancelRejected, EvBookingRefunded, EvTicketsIssued,
}

func (t BookingEventType) Valid() bool { return contains(AllBookingEventTypes, t) }

type PaymentEventType string

const (
	EvPaymentCreated         PaymentEventType = "PAYMENT_CREATED"
	EvPaymentCreateFailed    PaymentEventType = "PAYMENT_CREATE_FAILED"
	EvWebhookReceived        PaymentEventType = "WEBHOOK_RECEIVED"
	EvWebhookRejected        PaymentEventType = "WEBHOOK_REJECTED"
	EvPaymentSucceeded       PaymentEventType = "PAYMENT_SUCCEEDED"
	EvPaymentFailed          PaymentEventType = "PAYMENT_FAILED"
	EvPaymentDuplicateIgnore PaymentEventType = "PAYMENT_DUPLICATE_IGNORED"
	EvPaymentAmountMismatch  PaymentEventType = "PAYMENT_AMOUNT_MISMATCH"
	EvPaymentAfterExpiry     PaymentEventType = "PAYMENT_AFTER_EXPIRY"
	EvRefundRequested        PaymentEventType = "REFUND_REQUESTED"
	EvRefundCompleted        PaymentEventType = "REFUND_COMPLETED"
	EvRefundFailed           PaymentEventType = "REFUND_FAILED"
	EvRefundRejected         PaymentEventType = "REFUND_REJECTED"
)

var AllPaymentEventTypes = []PaymentEventType{
	EvPaymentCreated, EvPaymentCreateFailed, EvWebhookReceived, EvWebhookRejected,
	EvPaymentSucceeded, EvPaymentFailed, EvPaymentDuplicateIgnore, EvPaymentAmountMismatch,
	EvPaymentAfterExpiry, EvRefundRequested, EvRefundCompleted, EvRefundFailed, EvRefundRejected,
}

func (t PaymentEventType) Valid() bool { return contains(AllPaymentEventTypes, t) }

// ReasonCode is used both as the audit-log reason_code and as the API error code.
type ReasonCode string

const (
	// booking
	ReasonSeatUnavailable   ReasonCode = "SEAT_UNAVAILABLE"
	ReasonSeatNotInShowtime ReasonCode = "SEAT_NOT_IN_SHOWTIME"
	ReasonShowtimeNotOnSale ReasonCode = "SHOWTIME_NOT_ON_SALE"
	ReasonTooManySeats      ReasonCode = "TOO_MANY_SEATS"
	ReasonValidationFailed  ReasonCode = "VALIDATION_FAILED"
	ReasonHoldExpired       ReasonCode = "HOLD_EXPIRED"
	ReasonUserCancelled     ReasonCode = "USER_CANCELLED"
	ReasonBookingNotPending ReasonCode = "BOOKING_NOT_PENDING"
	ReasonBookingNotFound   ReasonCode = "BOOKING_NOT_FOUND"
	ReasonRedisFallbackUsed ReasonCode = "REDIS_FALLBACK_USED"
	ReasonInternalError     ReasonCode = "INTERNAL_ERROR"
	// payment
	ReasonInvalidSignature  ReasonCode = "INVALID_SIGNATURE"
	ReasonMalformedPayload  ReasonCode = "MALFORMED_PAYLOAD"
	ReasonUnknownPayment    ReasonCode = "UNKNOWN_PAYMENT"
	ReasonBookingNotPayable ReasonCode = "BOOKING_NOT_PAYABLE"
	ReasonBookingExpired    ReasonCode = "BOOKING_EXPIRED"
	ReasonSeatLost          ReasonCode = "SEAT_LOST"
	ReasonAmountMismatch    ReasonCode = "AMOUNT_MISMATCH"
	ReasonGatewayDeclined   ReasonCode = "GATEWAY_DECLINED"
	ReasonDuplicateEvent    ReasonCode = "DUPLICATE_EVENT"
	// refund
	ReasonRefundNotEligible   ReasonCode = "REFUND_NOT_ELIGIBLE"
	ReasonTicketAlreadyUsed   ReasonCode = "TICKET_ALREADY_USED"
	ReasonRefundAlreadyExists ReasonCode = "REFUND_ALREADY_EXISTS"
	ReasonProviderFailed      ReasonCode = "PROVIDER_FAILED"
	ReasonAmountExceeds       ReasonCode = "AMOUNT_EXCEEDS_PAYMENT"
	// check-in
	ReasonTicketVoid ReasonCode = "TICKET_VOID"
	// API-only codes (returned to clients, not used as audit reasons)
	ReasonUnauthenticated     ReasonCode = "UNAUTHENTICATED"
	ReasonForbidden           ReasonCode = "FORBIDDEN"
	ReasonNotFound            ReasonCode = "NOT_FOUND"
	ReasonRateLimited         ReasonCode = "RATE_LIMITED"
	ReasonEmailTaken          ReasonCode = "EMAIL_TAKEN"
	ReasonInvalidCredentials  ReasonCode = "INVALID_CREDENTIALS"
	ReasonIdempotencyRequired ReasonCode = "IDEMPOTENCY_KEY_REQUIRED"
)

// httpStatus maps every ReasonCode to its HTTP status. A test enforces that no code is missing.
var httpStatus = map[ReasonCode]int{
	ReasonSeatUnavailable:     http.StatusConflict,
	ReasonSeatNotInShowtime:   http.StatusBadRequest,
	ReasonShowtimeNotOnSale:   http.StatusUnprocessableEntity,
	ReasonTooManySeats:        http.StatusUnprocessableEntity,
	ReasonValidationFailed:    http.StatusBadRequest,
	ReasonHoldExpired:         http.StatusConflict,
	ReasonUserCancelled:       http.StatusConflict,
	ReasonBookingNotPending:   http.StatusConflict,
	ReasonBookingNotFound:     http.StatusNotFound,
	ReasonRedisFallbackUsed:   http.StatusInternalServerError, // informational, never returned
	ReasonInternalError:       http.StatusInternalServerError,
	ReasonInvalidSignature:    http.StatusUnauthorized,
	ReasonMalformedPayload:    http.StatusBadRequest,
	ReasonUnknownPayment:      http.StatusNotFound,
	ReasonBookingNotPayable:   http.StatusConflict,
	ReasonBookingExpired:      http.StatusConflict,
	ReasonSeatLost:            http.StatusConflict,
	ReasonAmountMismatch:      http.StatusUnprocessableEntity,
	ReasonGatewayDeclined:     http.StatusUnprocessableEntity,
	ReasonDuplicateEvent:      http.StatusConflict,
	ReasonRefundNotEligible:   http.StatusUnprocessableEntity,
	ReasonTicketAlreadyUsed:   http.StatusUnprocessableEntity,
	ReasonRefundAlreadyExists: http.StatusConflict,
	ReasonProviderFailed:      http.StatusBadGateway,
	ReasonAmountExceeds:       http.StatusUnprocessableEntity,
	ReasonTicketVoid:          http.StatusUnprocessableEntity,
	ReasonUnauthenticated:     http.StatusUnauthorized,
	ReasonForbidden:           http.StatusForbidden,
	ReasonNotFound:            http.StatusNotFound,
	ReasonRateLimited:         http.StatusTooManyRequests,
	ReasonEmailTaken:          http.StatusConflict,
	ReasonInvalidCredentials:  http.StatusUnauthorized,
	ReasonIdempotencyRequired: http.StatusBadRequest,
}

var AllReasonCodes = func() []ReasonCode {
	out := make([]ReasonCode, 0, len(httpStatus))
	for c := range httpStatus {
		out = append(out, c)
	}
	return out
}()

func (r ReasonCode) Valid() bool {
	_, ok := httpStatus[r]
	return ok
}

// ---- API error ----

type AppError struct {
	Code    ReasonCode
	Message string
	Status  int
	Err     error // optional wrapped cause (logged, never sent to the client)
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return string(e.Code) + ": " + e.Message + ": " + e.Err.Error()
	}
	return string(e.Code) + ": " + e.Message
}

func (e *AppError) Unwrap() error { return e.Err }

func StatusFor(code ReasonCode) int {
	if s, ok := httpStatus[code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

func NewError(code ReasonCode, message string) *AppError {
	return &AppError{Code: code, Message: message, Status: StatusFor(code)}
}

func contains[T comparable](list []T, v T) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
