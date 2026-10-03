package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"ticketbooking/internal/domain"
	"ticketbooking/internal/repository"
)

// MockGateway simulates a payment provider: it signs a notification and POSTs it over HTTP to the
// real webhook endpoint, so the webhook code path is exactly the one a real provider would hit.
type MockGateway struct {
	repo       *repository.PaymentRepository
	secret     []byte
	webhookURL string
	client     *http.Client
}

func NewMockGateway(repo *repository.PaymentRepository, webhookSecret, webhookURL string) *MockGateway {
	return &MockGateway{
		repo: repo, secret: []byte(webhookSecret), webhookURL: webhookURL,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// MockPayResult is what the webhook answered to the simulated notification.
type MockPayResult struct {
	WebhookStatus int             `json:"webhook_status"`
	WebhookBody   json.RawMessage `json:"webhook_body,omitempty"`
}

// Pay sends a "succeeded" (result "success") or "failed" (result "fail") notification for the payment,
// with the amount read from the database.
func (g *MockGateway) Pay(ctx context.Context, paymentID, result string) (*MockPayResult, error) {
	ctx, cancel := context.WithTimeout(ctx, paymentOpTimeout)
	defer cancel()
	if result != "success" && result != "fail" {
		return nil, domain.NewError(domain.ReasonValidationFailed, `result must be "success" or "fail"`)
	}
	u, err := uuid.Parse(paymentID)
	if err != nil {
		return nil, domain.NewError(domain.ReasonNotFound, "payment not found")
	}
	p, err := g.repo.ByID(ctx, u.String())
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, domain.NewError(domain.ReasonNotFound, "payment not found")
	}

	payload := map[string]any{
		"event_id":      uuid.NewString(),
		"payment_id":    p.ID,
		"status":        webhookStatusSucceeded,
		"amount_satang": p.AmountSatang,
		"provider_ref":  "mock_" + uuid.NewString(),
	}
	if result == "fail" {
		payload["status"] = webhookStatusFailed
		payload["failure_code"] = string(domain.ReasonGatewayDeclined)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", Sign(g.secret, body))
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, &domain.AppError{Code: domain.ReasonProviderFailed, Message: "webhook delivery failed",
			Status: domain.StatusFor(domain.ReasonProviderFailed), Err: err}
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("read webhook response: %w", err)
	}
	out := &MockPayResult{WebhookStatus: resp.StatusCode}
	if json.Valid(respBody) {
		out.WebhookBody = respBody
	}
	return out, nil
}
