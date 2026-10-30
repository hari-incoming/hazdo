package azdevops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListPendingPipelineApprovals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet ||
			r.URL.Path != "/org/project/_apis/pipelines/approvals" ||
			r.URL.Query().Get("state") != "pending" ||
			r.URL.Query().Get("top") != "100" ||
			r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected pending approvals request: %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{
			"count":2,
			"value":[
				{"id":"older","status":"pending","instructions":"Authorize the test service connection","createdOn":"2026-10-25T10:00:00Z"},
				{"id":"newer","status":"pending","instructions":"Authorize the production environment","createdOn":"2026-10-26T10:00:00Z"}
			]
		}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "test-pat")
	if err != nil {
		t.Fatal(err)
	}
	client.SetBaseURL(server.URL + "/org/project/_apis")
	approvals, err := client.ListPendingPipelineApprovals()
	if err != nil {
		t.Fatalf("ListPendingPipelineApprovals() error = %v", err)
	}
	if len(approvals) != 2 || approvals[0].ID != "newer" || approvals[1].ID != "older" {
		t.Fatalf("pending approvals = %+v, want newest first", approvals)
	}
	if approvals[0].Instructions != "Authorize the production environment" {
		t.Fatalf("approval instructions = %q", approvals[0].Instructions)
	}
}

func TestApprovePipelineResourceApproval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch ||
			r.URL.Path != "/org/project/_apis/pipelines/approvals" ||
			r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected approval update request: %s %s", r.Method, r.URL.String())
		}
		var payload []struct {
			ApprovalID string `json:"approvalId"`
			Status     string `json:"status"`
			Comment    string `json:"comment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode approval update: %v", err)
		}
		if len(payload) != 1 || payload[0].ApprovalID != "approval-123" ||
			payload[0].Status != "approved" || payload[0].Comment == "" {
			t.Errorf("approval update payload = %+v", payload)
		}
		_, _ = w.Write([]byte(`{"count":1,"value":[{"id":"approval-123","status":"approved"}]}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "test-pat")
	if err != nil {
		t.Fatal(err)
	}
	client.SetBaseURL(server.URL + "/org/project/_apis")
	if err := client.ApprovePipelineResourceApproval(" approval-123 "); err != nil {
		t.Fatalf("ApprovePipelineResourceApproval() error = %v", err)
	}
	if err := client.ApprovePipelineResourceApproval(" "); err == nil ||
		!strings.Contains(err.Error(), "ID is required") {
		t.Fatalf("empty approval ID error = %v, want validation error", err)
	}
}

func TestApprovePipelineResourceApprovalRequiresConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"count":1,"value":[{"id":"approval-123","status":"pending"}]}`))
	}))
	defer server.Close()
	client, err := NewClient("org", "project", "test-pat")
	if err != nil {
		t.Fatal(err)
	}
	client.SetBaseURL(server.URL + "/org/project/_apis")
	if err := client.ApprovePipelineResourceApproval("approval-123"); err == nil ||
		!strings.Contains(err.Error(), "status \"pending\"") {
		t.Fatalf("unconfirmed approval error = %v", err)
	}
}
