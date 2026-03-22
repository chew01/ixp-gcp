package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

// --- fakes for Server dependencies ---

type fakeFlowStore struct {
	val string
}

func (f *fakeFlowStore) Get(ctx context.Context, flowKey string) (string, error) {
	return f.val, nil
}

func (f *fakeFlowStore) List(ctx context.Context) ([]string, error) {
	return nil, nil
}

type fakeBidStore struct{}

func (f *fakeBidStore) Put(ctx context.Context, bid shared.BidRequest, customerID string) error {
	return nil
}

type fakeCreditsStore struct {
	cred shared.CustomerCredits
}

func (f *fakeCreditsStore) Get(ctx context.Context, customerID string) (shared.CustomerCredits, error) {
	return f.cred, nil
}

func (f *fakeCreditsStore) List(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeCreditsStore) AddSpent(ctx context.Context, customerID string, amount int) error {
	return nil
}

func (f *fakeCreditsStore) InitCustomerIfMissing(ctx context.Context, customerID string) error {
	return nil
}

type fakeHistoryStore struct{}

func (f *fakeHistoryStore) Get(ctx context.Context, key string) (string, error) {
	return "", nil
}

func (f *fakeHistoryStore) List(ctx context.Context) ([]string, error) {
	return nil, nil
}

// --- tests ---

func TestGetCredits_UsesTokenAndReturnsOwnCredits(t *testing.T) {
	s := &Server{
		cs: &fakeCreditsStore{
			cred: shared.CustomerCredits{TotalSpent: 123},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/credits", nil)
	req.Header.Set("X-Customer-ID", "as12345")
	rr := httptest.NewRecorder()

	s.getCredits(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp shared.CustomerCredits
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.TotalSpent != 123 {
		t.Fatalf("expected total_spent=123, got %d", resp.TotalSpent)
	}
}

func TestGetCredits_RequiresToken(t *testing.T) {
	s := &Server{
		cs: &fakeCreditsStore{},
	}

	req := httptest.NewRequest(http.MethodGet, "/credits", nil)
	rr := httptest.NewRecorder()

	s.getCredits(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rr.Code)
	}
}

func TestGetFlows_EnforcesCustomerOwnership(t *testing.T) {
	scene := &scenario.Scenario{
		Switches: []scenario.Switch{
			{ID: "sw-1", IngressPorts: []uint32{1}},
		},
		Customers: []scenario.Customer{
			{ID: "as12345", SwitchID: "sw-1", IngressPorts: []uint32{1}},
		},
	}

	s := &Server{
		fs:       &fakeFlowStore{val: `{"throughput_kbps": 100}`},
		bs:       &fakeBidStore{},
		cs:       &fakeCreditsStore{},
		hs:       &fakeHistoryStore{},
		scenario: scene,
	}

	// Allowed: customer owns ingress port 1.
	req := httptest.NewRequest(http.MethodGet, "/flows?switch_id=sw-1&ingress_port=1&egress_port=0", nil)
	req.Header.Set("X-Customer-ID", "as12345")
	rr := httptest.NewRecorder()

	s.getFlows(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	// Forbidden: different customer ID.
	req2 := httptest.NewRequest(http.MethodGet, "/flows?switch_id=sw-1&ingress_port=1&egress_port=0", nil)
	req2.Header.Set("X-Customer-ID", "as67890")
	rr2 := httptest.NewRecorder()

	s.getFlows(rr2, req2)

	if rr2.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rr2.Code)
	}
}

