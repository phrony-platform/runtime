package handlers

import (
	"context"
	"encoding/json"
	"testing"
)

func TestProcessPayment(t *testing.T) {
	raw, err := processPayment(context.Background(), json.RawMessage(`{"amount":42.5,"currency":"usd","payee":"Acme Corp"}`))
	if err != nil {
		t.Fatalf("tool error: %+v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "processed" {
		t.Fatalf("status = %v", out["status"])
	}
	if out["currency"] != "USD" {
		t.Fatalf("currency = %v", out["currency"])
	}
	if out["payee"] != "Acme Corp" {
		t.Fatalf("payee = %v", out["payee"])
	}
}

func TestProcessPayment_invalidAmount(t *testing.T) {
	_, terr := processPayment(context.Background(), json.RawMessage(`{"amount":0,"currency":"USD","payee":"Acme"}`))
	if terr == nil || terr.Code != "invalid_args" {
		t.Fatalf("tool error = %+v", terr)
	}
}

func TestProcessPayment_missingPayee(t *testing.T) {
	_, terr := processPayment(context.Background(), json.RawMessage(`{"amount":10,"currency":"USD"}`))
	if terr == nil || terr.Code != "invalid_args" {
		t.Fatalf("tool error = %+v", terr)
	}
}
