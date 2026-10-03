package domain

import "testing"

func TestEventTypesAreUniqueAndNonEmpty(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range AllBookingEventTypes {
		if v == "" || seen[string(v)] {
			t.Errorf("booking event type empty or duplicated: %q", v)
		}
		seen[string(v)] = true
	}
	seen = map[string]bool{}
	for _, v := range AllPaymentEventTypes {
		if v == "" || seen[string(v)] {
			t.Errorf("payment event type empty or duplicated: %q", v)
		}
		seen[string(v)] = true
	}
}

func TestEveryReasonCodeHasHTTPStatus(t *testing.T) {
	if len(AllReasonCodes) == 0 {
		t.Fatal("no reason codes")
	}
	for _, c := range AllReasonCodes {
		if c == "" {
			t.Error("empty reason code")
		}
		if StatusFor(c) < 400 {
			t.Errorf("reason code %s has non-error status %d", c, StatusFor(c))
		}
	}
}

func TestNewErrorUsesMappedStatus(t *testing.T) {
	e := NewError(ReasonSeatUnavailable, "seat taken")
	if e.Status != 409 {
		t.Fatalf("status = %d, want 409", e.Status)
	}
}

func TestValidators(t *testing.T) {
	if !OutcomeSuccess.Valid() || Outcome("MAYBE").Valid() {
		t.Error("Outcome.Valid is wrong")
	}
	if !ActorGateway.Valid() || ActorType("BOT").Valid() {
		t.Error("ActorType.Valid is wrong")
	}
	if !EvBookingCreated.Valid() || BookingEventType("X").Valid() {
		t.Error("BookingEventType.Valid is wrong")
	}
	if !EvWebhookReceived.Valid() || PaymentEventType("X").Valid() {
		t.Error("PaymentEventType.Valid is wrong")
	}
}
