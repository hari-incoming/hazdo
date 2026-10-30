package azdevops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hari-incoming/hazdo/internal/provider"
)

const pendingApprovalsLimit = 100

type approvalResponse struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"`
	Instructions string    `json:"instructions"`
	CreatedOn    time.Time `json:"createdOn"`
}

type approvalsResponse struct {
	Value []approvalResponse `json:"value"`
}

// ListPendingPipelineApprovals returns pending protected-resource approvals
// in this project. The API does not include a pipeline/run filter.
func (c *Client) ListPendingPipelineApprovals() ([]provider.PipelineApproval, error) {
	body, err := c.get(fmt.Sprintf(
		"/pipelines/approvals?state=pending&top=%d&api-version=7.1",
		pendingApprovalsLimit,
	))
	if err != nil {
		return nil, fmt.Errorf("failed to list pending pipeline approvals (PAT needs Pipeline Resources: Use): %w", err)
	}

	var response approvalsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse pending pipeline approvals: %w", err)
	}

	approvals := make([]provider.PipelineApproval, 0, len(response.Value))
	for _, approval := range response.Value {
		if approval.ID == "" {
			return nil, fmt.Errorf("Azure DevOps returned a pending approval without an ID")
		}
		approvals = append(approvals, provider.PipelineApproval{
			ID:           approval.ID,
			Status:       approval.Status,
			Instructions: approval.Instructions,
			CreatedOn:    approval.CreatedOn,
		})
	}
	sort.Slice(approvals, func(i, j int) bool {
		return approvals[i].CreatedOn.After(approvals[j].CreatedOn)
	})
	return approvals, nil
}

// ApprovePipelineResourceApproval approves one pending protected-resource
// request. Approvals are project-scoped and should only be submitted after
// the caller has shown the request instructions to the user.
func (c *Client) ApprovePipelineResourceApproval(approvalID string) error {
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return fmt.Errorf("pipeline approval ID is required")
	}

	requestBody, err := json.Marshal([]struct {
		ApprovalID string `json:"approvalId"`
		Status     string `json:"status"`
		Comment    string `json:"comment,omitempty"`
	}{{
		ApprovalID: approvalID,
		Status:     "approved",
		Comment:    "Approved from Hazdo",
	}})
	if err != nil {
		return fmt.Errorf("failed to encode pipeline approval: %w", err)
	}

	body, err := c.patch("/pipelines/approvals?api-version=7.1", bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("failed to approve pipeline resource request (PAT needs Pipeline Resources: Use): %w", err)
	}
	var response approvalsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse pipeline approval response: %w", err)
	}
	for _, approval := range response.Value {
		if strings.EqualFold(approval.ID, approvalID) {
			if approval.Status != "approved" {
				return fmt.Errorf("Azure DevOps returned approval %q with status %q", approvalID, approval.Status)
			}
			return nil
		}
	}
	return fmt.Errorf("Azure DevOps did not confirm approval %q", approvalID)
}
