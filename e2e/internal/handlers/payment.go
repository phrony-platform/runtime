package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/phrony-platform/runtime/e2e/internal/workclient"
)

type paymentArgs struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
	Payee    string  `json:"payee"`
}

func processPayment(_ context.Context, raw json.RawMessage) (json.RawMessage, *workclient.ToolError) {
	var args paymentArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, &workclient.ToolError{
				Code:    "invalid_args",
				Message: fmt.Sprintf("decode args: %v", err),
			}
		}
	}
	currency := strings.TrimSpace(strings.ToUpper(args.Currency))
	payee := strings.TrimSpace(args.Payee)
	if args.Amount <= 0 {
		return nil, &workclient.ToolError{
			Code:    "invalid_args",
			Message: "amount must be greater than zero",
		}
	}
	if currency == "" {
		return nil, &workclient.ToolError{
			Code:    "invalid_args",
			Message: "currency is required",
		}
	}
	if payee == "" {
		return nil, &workclient.ToolError{
			Code:    "invalid_args",
			Message: "payee is required",
		}
	}

	fmt.Printf("payment processed: %.2f %s to %s\n", args.Amount, currency, payee)

	payload, err := json.Marshal(map[string]any{
		"status":     "processed",
		"payment_id": fmt.Sprintf("pay-%s-%s", currency, payee),
		"amount":     args.Amount,
		"currency":   currency,
		"payee":      payee,
		"source":     "runtime-e2e",
	})
	if err != nil {
		return nil, &workclient.ToolError{Code: "internal", Message: err.Error()}
	}
	return payload, nil
}
