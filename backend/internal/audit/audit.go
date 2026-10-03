// Package audit is the ONLY place that writes booking_events and payment_events.
// Event tables are append-only (a database trigger rejects UPDATE/DELETE).
//
// Pass a transaction as db to record the event atomically with the state change.
// For failed attempts that rolled back, call it afterwards with the plain connection.
// Never put secrets (passwords, tokens, signatures, card data) into Metadata or RawPayload.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"ticketbooking/internal/domain"
)

type BookingEvent struct {
	BookingID  *string // nil when the attempt failed before a booking existed
	UserID     *string
	ShowtimeID *string
	EventType  domain.BookingEventType
	Outcome    domain.Outcome
	ReasonCode domain.ReasonCode // empty for plain successes
	FromStatus string
	ToStatus   string
	ActorType  domain.ActorType
	ActorID    *string
	Metadata   map[string]any
}

type PaymentEvent struct {
	PaymentID      *string // nil when a webhook references an unknown payment
	BookingID      *string
	EventType      domain.PaymentEventType
	Outcome        domain.Outcome
	ReasonCode     domain.ReasonCode
	FromStatus     string
	ToStatus       string
	AmountSatang   *int64
	ProviderRef    string
	SignatureValid *bool // store only true/false, never the signature itself
	ActorType      domain.ActorType
	ActorID        *string
	RawPayload     json.RawMessage // webhook body; empty means {}
}

func RecordBookingEvent(ctx context.Context, db *gorm.DB, e BookingEvent) error {
	if !e.EventType.Valid() {
		return fmt.Errorf("audit: invalid booking event type %q", e.EventType)
	}
	if !e.Outcome.Valid() {
		return fmt.Errorf("audit: invalid outcome %q", e.Outcome)
	}
	if !e.ActorType.Valid() {
		return fmt.Errorf("audit: invalid actor type %q", e.ActorType)
	}
	if e.ReasonCode != "" && !e.ReasonCode.Valid() {
		return fmt.Errorf("audit: invalid reason code %q", e.ReasonCode)
	}
	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("audit: marshal metadata: %w", err)
	}
	return db.WithContext(ctx).Exec(
		`INSERT INTO booking_events
		   (booking_id, user_id, showtime_id, event_type, outcome, reason_code,
		    from_status, to_status, actor_type, actor_id, metadata)
		 VALUES (?::uuid, ?::uuid, ?::uuid, ?, ?, ?, ?, ?, ?, ?::uuid, ?::jsonb)`,
		e.BookingID, e.UserID, e.ShowtimeID, string(e.EventType), string(e.Outcome), string(e.ReasonCode),
		e.FromStatus, e.ToStatus, string(e.ActorType), e.ActorID, string(metaJSON),
	).Error
}

func RecordPaymentEvent(ctx context.Context, db *gorm.DB, e PaymentEvent) error {
	if !e.EventType.Valid() {
		return fmt.Errorf("audit: invalid payment event type %q", e.EventType)
	}
	if !e.Outcome.Valid() {
		return fmt.Errorf("audit: invalid outcome %q", e.Outcome)
	}
	if !e.ActorType.Valid() {
		return fmt.Errorf("audit: invalid actor type %q", e.ActorType)
	}
	if e.ReasonCode != "" && !e.ReasonCode.Valid() {
		return fmt.Errorf("audit: invalid reason code %q", e.ReasonCode)
	}
	raw := string(e.RawPayload)
	if raw == "" || !json.Valid(e.RawPayload) {
		raw = "{}"
	}
	return db.WithContext(ctx).Exec(
		`INSERT INTO payment_events
		   (payment_id, booking_id, event_type, outcome, reason_code, from_status, to_status,
		    amount_satang, provider_ref, signature_valid, actor_type, actor_id, raw_payload)
		 VALUES (?::uuid, ?::uuid, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::uuid, ?::jsonb)`,
		e.PaymentID, e.BookingID, string(e.EventType), string(e.Outcome), string(e.ReasonCode),
		e.FromStatus, e.ToStatus, e.AmountSatang, e.ProviderRef, e.SignatureValid,
		string(e.ActorType), e.ActorID, raw,
	).Error
}
