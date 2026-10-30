package provider_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hari-incoming/hazdo/internal/provider"
)

// compile-time gate: CompositeProvider must satisfy provider.Provider.
var _ provider.Provider = (*provider.CompositeProvider)(nil)

// ---------------------------------------------------------------------------
// fakeBackend — a minimal stub Provider for use in composite tests.
//
// It returns canned results for every list method and records which scope was
// passed to routed (detail/mutation/URL) calls so tests can assert routing.
// ---------------------------------------------------------------------------

type fakeBackend struct {
	kind    provider.Kind
	scopes  []string
	prs     []provider.PullRequest
	myPRs   []provider.PullRequest
	revPRs  []provider.PullRequest
	items   []provider.WorkItem
	myItems []provider.WorkItem
	runs    []provider.PipelineRun
	listErr error // error to return from all list methods

	// routed call recording
	lastRouteScope string
}

type editableFakeBackend struct {
	*fakeBackend
	updatedDescription string
	updatedTitle       string
}

type completableFakeBackend struct {
	*fakeBackend
	canComplete bool
	repository  string
	pullRequest int
	strategy    provider.PRMergeStrategy
}

type abandonableFakeBackend struct {
	*fakeBackend
	canAbandon  bool
	repository  string
	pullRequest int
}

type workItemCreatorFake struct {
	*fakeBackend
	creationScopes  []string
	createdScope    string
	createdType     string
	createdTitle    string
	createdDesc     string
	createdAssignee string
}

type pipelineApprovalFake struct {
	*fakeBackend
	approvals  []provider.PipelineApproval
	approvedID string
}

func (*pipelineApprovalFake) HasPipelineApprovals() bool { return true }

func (f *pipelineApprovalFake) ListPendingPipelineApprovals(scope string) ([]provider.PipelineApproval, error) {
	f.lastRouteScope = scope
	return f.approvals, nil
}

func (f *pipelineApprovalFake) ApprovePipelineResourceApproval(scope, approvalID string) error {
	f.lastRouteScope = scope
	f.approvedID = approvalID
	return nil
}

func (f *workItemCreatorFake) WorkItemCreationScopes() []string {
	return f.creationScopes
}

func (f *workItemCreatorFake) CreateWorkItem(scope, itemType, title, description, assignedTo string) (*provider.WorkItem, error) {
	f.createdScope = scope
	f.createdType = itemType
	f.createdTitle = title
	f.createdDesc = description
	f.createdAssignee = assignedTo
	return &provider.WorkItem{Identity: provider.Identity{Scope: scope, ID: "42"}}, nil
}

func TestCompositePipelineApprovalsRouteByScopeAndCapability(t *testing.T) {
	azure := &pipelineApprovalFake{
		fakeBackend: &fakeBackend{kind: provider.KindAzure, scopes: []string{"project-a"}},
		approvals: []provider.PipelineApproval{{
			ID: "approval-1", Status: "pending",
		}},
	}
	github := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"owner/repo"}}
	composite := provider.NewCompositeProvider(azure, github)
	if !composite.HasPipelineApprovals() {
		t.Fatal("composite should report Azure approval capability")
	}
	approvals, err := composite.ListPendingPipelineApprovals("project-a")
	if err != nil || len(approvals) != 1 || approvals[0].ID != "approval-1" || azure.lastRouteScope != "project-a" {
		t.Fatalf("routed approvals = %+v, scope %q, error %v", approvals, azure.lastRouteScope, err)
	}
	if err := composite.ApprovePipelineResourceApproval("project-a", "approval-1"); err != nil {
		t.Fatalf("ApprovePipelineResourceApproval() error = %v", err)
	}
	if azure.approvedID != "approval-1" || azure.lastRouteScope != "project-a" {
		t.Fatalf("approval routed to %q/%q, want project-a/approval-1", azure.lastRouteScope, azure.approvedID)
	}
	if _, err := composite.ListPendingPipelineApprovals("owner/repo"); err == nil {
		t.Fatal("GitHub backend should not expose Azure pipeline approvals")
	}
	if provider.NewCompositeProvider(github).HasPipelineApprovals() {
		t.Fatal("GitHub-only composite should not report Azure approval capability")
	}
}

func (f *completableFakeBackend) CanCompletePullRequest(scope string) bool {
	if !f.canComplete {
		return false
	}
	for _, configuredScope := range f.scopes {
		if configuredScope == scope {
			return true
		}
	}
	return false
}

func (f *completableFakeBackend) CompletePullRequest(scope, repositoryID string, pullRequestID int, strategy provider.PRMergeStrategy) error {
	f.lastRouteScope = scope
	f.repository = repositoryID
	f.pullRequest = pullRequestID
	f.strategy = strategy
	return nil
}

func (f *abandonableFakeBackend) CanAbandonPullRequest(scope string) bool {
	if !f.canAbandon {
		return false
	}
	for _, configuredScope := range f.scopes {
		if configuredScope == scope {
			return true
		}
	}
	return false
}

func (f *abandonableFakeBackend) AbandonPullRequest(scope, repositoryID string, pullRequestID int) error {
	f.lastRouteScope = scope
	f.repository = repositoryID
	f.pullRequest = pullRequestID
	return nil
}

func (f *editableFakeBackend) CanEditPullRequest(scope string) bool {
	for _, configuredScope := range f.scopes {
		if configuredScope == scope {
			return true
		}
	}
	return false
}

func TestCompositeProvider_PRCompleterRoutesAndChecksCapability(t *testing.T) {
	azure := &completableFakeBackend{
		fakeBackend: &fakeBackend{kind: provider.KindAzure, scopes: []string{"project"}},
		canComplete: true,
	}
	github := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"owner/repo"}}
	composite := provider.NewCompositeProvider(azure, github)

	if !composite.CanCompletePullRequest("project") {
		t.Fatal("CanCompletePullRequest(project) = false, want true")
	}
	if composite.CanCompletePullRequest("owner/repo") {
		t.Fatal("CanCompletePullRequest(owner/repo) = true, want false")
	}
	if err := composite.CompletePullRequest("project", "repo-id", 42, provider.PRMergeSquash); err != nil {
		t.Fatalf("CompletePullRequest() error = %v", err)
	}
	if azure.lastRouteScope != "project" || azure.repository != "repo-id" ||
		azure.pullRequest != 42 || azure.strategy != provider.PRMergeSquash {
		t.Errorf("completion route = %q/%q/%d/%q", azure.lastRouteScope, azure.repository, azure.pullRequest, azure.strategy)
	}
	if err := composite.CompletePullRequest("owner/repo", "repo-id", 42, provider.PRMergeSquash); err == nil {
		t.Fatal("CompletePullRequest() succeeded for a backend without the capability")
	}
}

func TestCompositeProvider_PRAbandonerRoutesAndChecksCapability(t *testing.T) {
	azure := &abandonableFakeBackend{
		fakeBackend: &fakeBackend{kind: provider.KindAzure, scopes: []string{"project"}},
		canAbandon:  true,
	}
	github := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"owner/repo"}}
	composite := provider.NewCompositeProvider(azure, github)

	if !composite.CanAbandonPullRequest("project") {
		t.Fatal("CanAbandonPullRequest(project) = false, want true")
	}
	if composite.CanAbandonPullRequest("owner/repo") {
		t.Fatal("CanAbandonPullRequest(owner/repo) = true, want false")
	}
	if err := composite.AbandonPullRequest("project", "repo-id", 42); err != nil {
		t.Fatalf("AbandonPullRequest() error = %v", err)
	}
	if azure.lastRouteScope != "project" || azure.repository != "repo-id" || azure.pullRequest != 42 {
		t.Errorf("abandon route = %q/%q/%d", azure.lastRouteScope, azure.repository, azure.pullRequest)
	}
	if err := composite.AbandonPullRequest("owner/repo", "repo-id", 42); err == nil {
		t.Fatal("AbandonPullRequest() succeeded for a backend without the capability")
	}
}

func TestCompositeProvider_WorkItemCreationRoutesOnlySupportedScopes(t *testing.T) {
	first := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"shared"}}
	azure := &workItemCreatorFake{
		fakeBackend:    &fakeBackend{kind: provider.KindAzure, scopes: []string{"shared", "project"}},
		creationScopes: []string{"shared", "project"},
	}
	composite := provider.NewCompositeProvider(first, azure)

	scopes := composite.WorkItemCreationScopes()
	if len(scopes) != 1 || scopes[0] != "project" {
		t.Fatalf("WorkItemCreationScopes() = %v, want [project]", scopes)
	}

	item, err := composite.CreateWorkItem("project", "Task", "Title", "Description", "user@example.com")
	if err != nil {
		t.Fatalf("CreateWorkItem() error = %v", err)
	}
	if item.Identity.ID != "42" ||
		azure.createdScope != "project" ||
		azure.createdType != "Task" ||
		azure.createdTitle != "Title" ||
		azure.createdDesc != "Description" ||
		azure.createdAssignee != "user@example.com" {
		t.Errorf("created item or routed arguments are incorrect: item=%#v scope=%q type=%q title=%q description=%q",
			item, azure.createdScope, azure.createdType, azure.createdTitle, azure.createdDesc)
	}
	if _, err := composite.CreateWorkItem("shared", "Task", "Title", "", ""); err == nil {
		t.Fatal("CreateWorkItem(shared) error = nil, want unsupported-backend error")
	}
}

func (f *editableFakeBackend) UpdatePullRequestDescription(scope, _ string, _ int, description string) error {
	f.lastRouteScope = scope
	f.updatedDescription = description
	return nil
}

func (f *editableFakeBackend) UpdatePullRequestTitle(scope, _ string, _ int, title string) error {
	f.lastRouteScope = scope
	f.updatedTitle = title
	return nil
}

func (f *fakeBackend) Kind() provider.Kind  { return f.kind }
func (f *fakeBackend) Scopes() []string     { return f.scopes }
func (f *fakeBackend) IsMultiProject() bool { return len(f.scopes) > 1 }

func (f *fakeBackend) ListPullRequests(_ int, _ provider.ListOpts) ([]provider.PullRequest, error) {
	return f.prs, f.listErr
}
func (f *fakeBackend) ListMyPullRequests(_ int, _ provider.ListOpts) ([]provider.PullRequest, error) {
	return f.myPRs, f.listErr
}
func (f *fakeBackend) ListPullRequestsAsReviewer(_ int, _ provider.ListOpts) ([]provider.PullRequest, error) {
	return f.revPRs, f.listErr
}
func (f *fakeBackend) ListWorkItems(_ int, _ provider.ListOpts) ([]provider.WorkItem, error) {
	return f.items, f.listErr
}
func (f *fakeBackend) ListMyWorkItems(_ int, _ provider.ListOpts) ([]provider.WorkItem, error) {
	return f.myItems, f.listErr
}
func (f *fakeBackend) ListPipelineRuns(_ int, _ provider.ListOpts) ([]provider.PipelineRun, error) {
	return f.runs, f.listErr
}

// Detail / mutation methods — record the scope for routing assertions.

func (f *fakeBackend) GetPRThreads(scope, _ string, _ int) ([]provider.Thread, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) GetPRIterations(scope, _ string, _ int) ([]provider.Iteration, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) GetPRIterationChanges(scope, _ string, _ int, _ int) ([]provider.IterationChange, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) VotePullRequest(scope, _ string, _ int, _ int) error {
	f.lastRouteScope = scope
	return nil
}
func (f *fakeBackend) GetFileContent(scope, _ string, _ string, _ string) (string, error) {
	f.lastRouteScope = scope
	return "content", nil
}
func (f *fakeBackend) AddPRCodeComment(scope, _ string, _ int, _ string, _ int, _ string) (*provider.Thread, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) AddPRComment(scope, _ string, _ int, _ string) (*provider.Thread, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) ReplyToThread(scope, _ string, _ int, _ int, _ string) (*provider.Comment, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) UpdateThreadStatus(scope, _ string, _ int, _ int, _ string) error {
	f.lastRouteScope = scope
	return nil
}
func (f *fakeBackend) GetWorkItemTypeStates(scope, _ string) ([]provider.WorkItemTypeState, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) UpdateWorkItemState(scope string, _ int, _ string) error {
	f.lastRouteScope = scope
	return nil
}
func (f *fakeBackend) GetWorkItemComments(scope string, _ int) ([]provider.WorkItemComment, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) AddWorkItemComment(scope string, _ int, _ string) (*provider.WorkItemComment, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) GetBuildTimeline(scope string, _ int) (*provider.Timeline, error) {
	f.lastRouteScope = scope
	return nil, nil
}
func (f *fakeBackend) GetBuildLogContent(scope string, _, _ int) (string, error) {
	f.lastRouteScope = scope
	return "", nil
}
func (f *fakeBackend) WorkItemURL(scope string, _ int) string {
	f.lastRouteScope = scope
	return "wi-url:" + scope
}
func (f *fakeBackend) PRURL(scope, _ string, _ int) string {
	f.lastRouteScope = scope
	return "pr-url:" + scope
}
func (f *fakeBackend) PRThreadWebURL(scope, _ string, _ int, _ int) string {
	f.lastRouteScope = scope
	return "thread-url:" + scope
}
func (f *fakeBackend) PipelineURL(scope string, _ int) string {
	f.lastRouteScope = scope
	return "pipe-url:" + scope
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makePR(scope string, ts time.Time) provider.PullRequest {
	return provider.PullRequest{
		Identity:     provider.Identity{Scope: scope},
		CreationDate: ts,
	}
}

func TestCompositeProvider_UpdatePullRequestDescriptionRoutesAndChecksCapability(t *testing.T) {
	backend := &editableFakeBackend{fakeBackend: &fakeBackend{
		kind:   provider.KindAzure,
		scopes: []string{"project-a"},
	}}
	composite := provider.NewCompositeProvider(backend)

	if !composite.CanEditPullRequest("project-a") {
		t.Fatal("CanEditPullRequest(project-a) = false, want true")
	}
	if composite.CanEditPullRequest("missing") {
		t.Fatal("CanEditPullRequest(missing) = true, want false")
	}

	description := "first line\nsecond line"
	if err := composite.UpdatePullRequestDescription("project-a", "repo-id", 17, description); err != nil {
		t.Fatalf("UpdatePullRequestDescription() error = %v", err)
	}
	if backend.lastRouteScope != "project-a" || backend.updatedDescription != description {
		t.Fatalf("routed scope=%q description=%q", backend.lastRouteScope, backend.updatedDescription)
	}
	if err := composite.UpdatePullRequestTitle("project-a", "repo-id", 17, "Updated title"); err != nil {
		t.Fatalf("UpdatePullRequestTitle() error = %v", err)
	}
	if backend.lastRouteScope != "project-a" || backend.updatedTitle != "Updated title" {
		t.Fatalf("routed scope=%q title=%q", backend.lastRouteScope, backend.updatedTitle)
	}

	unsupported := provider.NewCompositeProvider(&fakeBackend{
		kind:   provider.KindGitHub,
		scopes: []string{"owner/repo"},
	})
	if err := unsupported.UpdatePullRequestDescription("owner/repo", "repo", 17, description); err == nil {
		t.Fatal("UpdatePullRequestDescription() succeeded for unsupported backend")
	}
}

func makeItem(scope string, ts time.Time) provider.WorkItem {
	return provider.WorkItem{
		Identity:    provider.Identity{Scope: scope},
		ChangedDate: ts,
	}
}

func makeRun(scope string, ts time.Time) provider.PipelineRun {
	return provider.PipelineRun{
		Identity:  provider.Identity{Scope: scope},
		QueueTime: ts,
	}
}

var (
	t1 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	t3 = time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC)
)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestCompositeProvider_SingleBackend_Transparency verifies D1: a
// single-backend composite returns results in the same order as the backend.
func TestCompositeProvider_SingleBackend_Transparency(t *testing.T) {
	b := &fakeBackend{
		kind:   provider.KindAzure,
		scopes: []string{"ProjectA"},
		prs:    []provider.PullRequest{makePR("ProjectA", t3), makePR("ProjectA", t2)},
		items:  []provider.WorkItem{makeItem("ProjectA", t3), makeItem("ProjectA", t2)},
		runs:   []provider.PipelineRun{makeRun("ProjectA", t3), makeRun("ProjectA", t2)},
	}
	cp := provider.NewCompositeProvider(b)

	t.Run("ListPullRequests", func(t *testing.T) {
		prs, err := cp.ListPullRequests(10, provider.ListOpts{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(prs) != 2 {
			t.Fatalf("want 2 PRs, got %d", len(prs))
		}
		if !prs[0].CreationDate.Equal(t3) || !prs[1].CreationDate.Equal(t2) {
			t.Errorf("want [t3,t2], got [%v,%v]", prs[0].CreationDate, prs[1].CreationDate)
		}
	})

	t.Run("ListWorkItems", func(t *testing.T) {
		items, err := cp.ListWorkItems(10, provider.ListOpts{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("want 2 items, got %d", len(items))
		}
		if !items[0].ChangedDate.Equal(t3) || !items[1].ChangedDate.Equal(t2) {
			t.Errorf("want [t3,t2], got [%v,%v]", items[0].ChangedDate, items[1].ChangedDate)
		}
	})

	t.Run("ListPipelineRuns", func(t *testing.T) {
		runs, err := cp.ListPipelineRuns(10, provider.ListOpts{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(runs) != 2 {
			t.Fatalf("want 2 runs, got %d", len(runs))
		}
		if !runs[0].QueueTime.Equal(t3) || !runs[1].QueueTime.Equal(t2) {
			t.Errorf("want [t3,t2], got [%v,%v]", runs[0].QueueTime, runs[1].QueueTime)
		}
	})
}

// TestCompositeProvider_TwoBackends_MergeAndSort verifies that results from
// two backends are interleaved and sorted by date descending.
func TestCompositeProvider_TwoBackends_MergeAndSort(t *testing.T) {
	// backend A owns t3 and t1; backend B owns t2.
	// Expected merged order: t3, t2, t1.
	a := &fakeBackend{
		kind:   provider.KindAzure,
		scopes: []string{"ScopeA"},
		prs:    []provider.PullRequest{makePR("ScopeA", t3), makePR("ScopeA", t1)},
		items:  []provider.WorkItem{makeItem("ScopeA", t3), makeItem("ScopeA", t1)},
		runs:   []provider.PipelineRun{makeRun("ScopeA", t3), makeRun("ScopeA", t1)},
	}
	bk := &fakeBackend{
		kind:   provider.KindGitHub,
		scopes: []string{"owner/repo"},
		prs:    []provider.PullRequest{makePR("owner/repo", t2)},
		items:  []provider.WorkItem{makeItem("owner/repo", t2)},
		runs:   []provider.PipelineRun{makeRun("owner/repo", t2)},
	}
	cp := provider.NewCompositeProvider(a, bk)

	t.Run("ListPullRequests", func(t *testing.T) {
		prs, err := cp.ListPullRequests(10, provider.ListOpts{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(prs) != 3 {
			t.Fatalf("want 3, got %d", len(prs))
		}
		wantTimes := []time.Time{t3, t2, t1}
		for i, w := range wantTimes {
			if !prs[i].CreationDate.Equal(w) {
				t.Errorf("prs[%d] want %v, got %v", i, w, prs[i].CreationDate)
			}
		}
	})

	t.Run("ListWorkItems", func(t *testing.T) {
		items, err := cp.ListWorkItems(10, provider.ListOpts{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("want 3, got %d", len(items))
		}
		wantTimes := []time.Time{t3, t2, t1}
		for i, w := range wantTimes {
			if !items[i].ChangedDate.Equal(w) {
				t.Errorf("items[%d] want %v, got %v", i, w, items[i].ChangedDate)
			}
		}
	})

	t.Run("ListPipelineRuns", func(t *testing.T) {
		runs, err := cp.ListPipelineRuns(10, provider.ListOpts{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(runs) != 3 {
			t.Fatalf("want 3, got %d", len(runs))
		}
		wantTimes := []time.Time{t3, t2, t1}
		for i, w := range wantTimes {
			if !runs[i].QueueTime.Equal(w) {
				t.Errorf("runs[%d] want %v, got %v", i, w, runs[i].QueueTime)
			}
		}
	})
}

// TestCompositeProvider_PartialFailure verifies that when one backend errors
// and the other succeeds, the healthy results are returned AND a *PartialError
// is also returned.
func TestCompositeProvider_PartialFailure(t *testing.T) {
	healthy := &fakeBackend{
		kind:    provider.KindAzure,
		scopes:  []string{"Good"},
		prs:     []provider.PullRequest{makePR("Good", t1)},
		items:   []provider.WorkItem{makeItem("Good", t1)},
		runs:    []provider.PipelineRun{makeRun("Good", t1)},
		listErr: nil,
	}
	broken := &fakeBackend{
		kind:    provider.KindGitHub,
		scopes:  []string{"Bad"},
		listErr: errors.New("backend down"),
	}
	cp := provider.NewCompositeProvider(healthy, broken)

	t.Run("ListPullRequests", func(t *testing.T) {
		prs, err := cp.ListPullRequests(10, provider.ListOpts{})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		var pe *provider.PartialError
		if !errors.As(err, &pe) {
			t.Fatalf("want *PartialError, got %T: %v", err, err)
		}
		if pe.Failed != 1 || pe.Total != 2 {
			t.Errorf("want Failed=1 Total=2, got Failed=%d Total=%d", pe.Failed, pe.Total)
		}
		if len(prs) != 1 {
			t.Errorf("want 1 healthy PR, got %d", len(prs))
		}
	})

	t.Run("ListWorkItems", func(t *testing.T) {
		items, err := cp.ListWorkItems(10, provider.ListOpts{})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		var pe *provider.PartialError
		if !errors.As(err, &pe) {
			t.Fatalf("want *PartialError, got %T: %v", err, err)
		}
		if len(items) != 1 {
			t.Errorf("want 1 healthy item, got %d", len(items))
		}
	})

	t.Run("ListPipelineRuns", func(t *testing.T) {
		runs, err := cp.ListPipelineRuns(10, provider.ListOpts{})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		var pe *provider.PartialError
		if !errors.As(err, &pe) {
			t.Fatalf("want *PartialError, got %T: %v", err, err)
		}
		if len(runs) != 1 {
			t.Errorf("want 1 healthy run, got %d", len(runs))
		}
	})
}

// TestCompositeProvider_AllFail verifies that when all backends error, a plain
// error is returned and results are nil.
func TestCompositeProvider_AllFail(t *testing.T) {
	b1 := &fakeBackend{kind: provider.KindAzure, scopes: []string{"A"}, listErr: errors.New("err1")}
	b2 := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"B"}, listErr: errors.New("err2")}
	cp := provider.NewCompositeProvider(b1, b2)

	t.Run("ListPullRequests", func(t *testing.T) {
		prs, err := cp.ListPullRequests(10, provider.ListOpts{})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		var pe *provider.PartialError
		if errors.As(err, &pe) {
			t.Fatalf("want plain error (not *PartialError), got *PartialError")
		}
		if prs != nil {
			t.Errorf("want nil results, got %v", prs)
		}
	})

	t.Run("ListWorkItems", func(t *testing.T) {
		items, err := cp.ListWorkItems(10, provider.ListOpts{})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		var pe *provider.PartialError
		if errors.As(err, &pe) {
			t.Fatal("want plain error, got *PartialError")
		}
		if items != nil {
			t.Errorf("want nil results, got %v", items)
		}
	})

	t.Run("ListPipelineRuns", func(t *testing.T) {
		runs, err := cp.ListPipelineRuns(10, provider.ListOpts{})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		var pe *provider.PartialError
		if errors.As(err, &pe) {
			t.Fatal("want plain error, got *PartialError")
		}
		if runs != nil {
			t.Errorf("want nil results, got %v", runs)
		}
	})
}

// TestCompositeProvider_Routing verifies that detail/mutation/URL calls with a
// given scope reach the backend that registered that scope.
func TestCompositeProvider_Routing(t *testing.T) {
	a := &fakeBackend{kind: provider.KindAzure, scopes: []string{"ProjectA"}}
	b := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"owner/repo"}}
	cp := provider.NewCompositeProvider(a, b)

	t.Run("ScopeA routes to backend a", func(t *testing.T) {
		_, _ = cp.GetPRThreads("ProjectA", "repo", 1)
		if a.lastRouteScope != "ProjectA" {
			t.Errorf("want a.lastRouteScope=%q, got %q", "ProjectA", a.lastRouteScope)
		}
		if b.lastRouteScope == "ProjectA" {
			t.Error("backend b should not have been called for ProjectA")
		}
	})

	t.Run("owner/repo routes to backend b", func(t *testing.T) {
		b.lastRouteScope = "" // reset
		_, _ = cp.GetPRThreads("owner/repo", "repo", 1)
		if b.lastRouteScope != "owner/repo" {
			t.Errorf("want b.lastRouteScope=%q, got %q", "owner/repo", b.lastRouteScope)
		}
	})

	t.Run("WorkItemURL routes correctly", func(t *testing.T) {
		url := cp.WorkItemURL("ProjectA", 42)
		if url != "wi-url:ProjectA" {
			t.Errorf("want %q, got %q", "wi-url:ProjectA", url)
		}
	})

	t.Run("PRURL routes correctly", func(t *testing.T) {
		url := cp.PRURL("owner/repo", "repo", 7)
		if url != "pr-url:owner/repo" {
			t.Errorf("want %q, got %q", "pr-url:owner/repo", url)
		}
	})
}

// TestCompositeProvider_UnknownScope verifies that calls with an unregistered
// scope return an error (or "" for URL methods).
func TestCompositeProvider_UnknownScope(t *testing.T) {
	a := &fakeBackend{kind: provider.KindAzure, scopes: []string{"ProjectA"}}
	cp := provider.NewCompositeProvider(a)

	t.Run("error-returning method returns error naming the scope", func(t *testing.T) {
		_, err := cp.GetPRThreads("UnknownScope", "repo", 1)
		if err == nil {
			t.Fatal("want error for unknown scope, got nil")
		}
		if !strings.Contains(err.Error(), "UnknownScope") {
			t.Errorf("want error message to name the unknown scope, got %q", err.Error())
		}
	})

	t.Run("URL method returns empty string", func(t *testing.T) {
		url := cp.WorkItemURL("UnknownScope", 1)
		if url != "" {
			t.Errorf("want empty string for unknown scope URL, got %q", url)
		}
	})

	t.Run("PRURL returns empty string", func(t *testing.T) {
		url := cp.PRURL("UnknownScope", "repo", 1)
		if url != "" {
			t.Errorf("want empty string, got %q", url)
		}
	})

	t.Run("PRThreadWebURL returns empty string", func(t *testing.T) {
		url := cp.PRThreadWebURL("UnknownScope", "repo", 1, 1)
		if url != "" {
			t.Errorf("want empty string, got %q", url)
		}
	})

	t.Run("PipelineURL returns empty string", func(t *testing.T) {
		url := cp.PipelineURL("UnknownScope", 1)
		if url != "" {
			t.Errorf("want empty string, got %q", url)
		}
	})
}

// TestCompositeProvider_ScopeCollision verifies D3: when two backends both
// claim the same scope, the first-registered backend handles routed calls.
func TestCompositeProvider_ScopeCollision(t *testing.T) {
	first := &fakeBackend{kind: provider.KindAzure, scopes: []string{"dup"}}
	second := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"dup", "unique"}}
	cp := provider.NewCompositeProvider(first, second)

	// "dup" should route to first.
	_ = cp.UpdateWorkItemState("dup", 1, "Active")
	if first.lastRouteScope != "dup" {
		t.Errorf("want first backend to handle 'dup', got lastRouteScope=%q", first.lastRouteScope)
	}
	if second.lastRouteScope == "dup" {
		t.Error("second backend should NOT handle 'dup' (collision: first wins)")
	}

	// "unique" still routes to second.
	second.lastRouteScope = ""
	_ = cp.UpdateWorkItemState("unique", 1, "Active")
	if second.lastRouteScope != "unique" {
		t.Errorf("want second backend to handle 'unique', got %q", second.lastRouteScope)
	}
}

// TestCompositeProvider_Kind verifies D4: single backend returns that
// backend's Kind; multiple backends return the first backend's Kind.
func TestCompositeProvider_Kind(t *testing.T) {
	t.Run("single backend", func(t *testing.T) {
		b := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"s"}}
		cp := provider.NewCompositeProvider(b)
		if got := cp.Kind(); got != provider.KindGitHub {
			t.Errorf("want KindGitHub, got %v", got)
		}
	})

	t.Run("multiple backends returns first", func(t *testing.T) {
		b1 := &fakeBackend{kind: provider.KindAzure, scopes: []string{"A"}}
		b2 := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"B"}}
		cp := provider.NewCompositeProvider(b1, b2)
		if got := cp.Kind(); got != provider.KindAzure {
			t.Errorf("want KindAzure (first), got %v", got)
		}
	})
}

// TestCompositeProvider_IsMultiProject verifies that IsMultiProject is true
// when the union of all backends' scopes spans more than one scope.
func TestCompositeProvider_IsMultiProject(t *testing.T) {
	t.Run("single scope => false", func(t *testing.T) {
		b := &fakeBackend{kind: provider.KindAzure, scopes: []string{"ProjectA"}}
		cp := provider.NewCompositeProvider(b)
		if cp.IsMultiProject() {
			t.Error("want false for single scope")
		}
	})

	t.Run("two scopes (same backend) => true", func(t *testing.T) {
		b := &fakeBackend{kind: provider.KindAzure, scopes: []string{"A", "B"}}
		cp := provider.NewCompositeProvider(b)
		if !cp.IsMultiProject() {
			t.Error("want true for two scopes")
		}
	})

	t.Run("two backends one scope each => true", func(t *testing.T) {
		b1 := &fakeBackend{kind: provider.KindAzure, scopes: []string{"A"}}
		b2 := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"B"}}
		cp := provider.NewCompositeProvider(b1, b2)
		if !cp.IsMultiProject() {
			t.Error("want true for two scopes across backends")
		}
	})

	t.Run("collision reduces total => false for one unique scope", func(t *testing.T) {
		b1 := &fakeBackend{kind: provider.KindAzure, scopes: []string{"dup"}}
		b2 := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"dup"}}
		cp := provider.NewCompositeProvider(b1, b2)
		// Both claim "dup", so only one unique scope in the routing map.
		if cp.IsMultiProject() {
			t.Error("want false: only one unique scope despite two backends")
		}
	})
}

// TestCompositeProvider_Scopes verifies that Scopes() returns the union of
// all backends' scopes in registration order with no duplicates.
func TestCompositeProvider_Scopes(t *testing.T) {
	b1 := &fakeBackend{kind: provider.KindAzure, scopes: []string{"A", "B"}}
	b2 := &fakeBackend{kind: provider.KindGitHub, scopes: []string{"C", "A"}} // "A" collides
	cp := provider.NewCompositeProvider(b1, b2)

	got := cp.Scopes()
	// Expected: A (from b1), B (from b1), C (from b2). "A" from b2 is skipped (dup).
	want := []string{"A", "B", "C"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("scopes[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

// TestCompositeProvider_MyPRs verifies ListMyPullRequests fan-out.
func TestCompositeProvider_MyPRs(t *testing.T) {
	a := &fakeBackend{
		kind:   provider.KindAzure,
		scopes: []string{"A"},
		myPRs:  []provider.PullRequest{makePR("A", t1)},
	}
	b := &fakeBackend{
		kind:   provider.KindGitHub,
		scopes: []string{"B"},
		myPRs:  []provider.PullRequest{makePR("B", t3)},
	}
	cp := provider.NewCompositeProvider(a, b)

	prs, err := cp.ListMyPullRequests(10, provider.ListOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("want 2, got %d", len(prs))
	}
	// t3 should be first
	if !prs[0].CreationDate.Equal(t3) {
		t.Errorf("want first PR at t3, got %v", prs[0].CreationDate)
	}
}

// TestCompositeProvider_PRsAsReviewer verifies ListPullRequestsAsReviewer fan-out.
func TestCompositeProvider_PRsAsReviewer(t *testing.T) {
	a := &fakeBackend{
		kind:   provider.KindAzure,
		scopes: []string{"A"},
		revPRs: []provider.PullRequest{makePR("A", t2)},
	}
	cp := provider.NewCompositeProvider(a)

	prs, err := cp.ListPullRequestsAsReviewer(10, provider.ListOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("want 1, got %d", len(prs))
	}
}

// TestCompositeProvider_SortIdempotent verifies D1: merge+sort is idempotent
// over already-sorted data (single backend case).
func TestCompositeProvider_SortIdempotent(t *testing.T) {
	// Already sorted descending: t3, t2, t1.
	b := &fakeBackend{
		kind:   provider.KindAzure,
		scopes: []string{"P"},
		prs: []provider.PullRequest{
			makePR("P", t3),
			makePR("P", t2),
			makePR("P", t1),
		},
	}
	cp := provider.NewCompositeProvider(b)

	prs, err := cp.ListPullRequests(10, provider.ListOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantTimes := []time.Time{t3, t2, t1}
	for i, w := range wantTimes {
		if !prs[i].CreationDate.Equal(w) {
			t.Errorf("prs[%d]: want %v, got %v", i, w, prs[i].CreationDate)
		}
	}
}

// TestCompositeProvider_RoutingAllMethods spot-checks several more routed
// methods to confirm they all route by scope.
func TestCompositeProvider_RoutingAllMethods(t *testing.T) {
	a := &fakeBackend{kind: provider.KindAzure, scopes: []string{"X"}}
	cp := provider.NewCompositeProvider(a)

	cases := []struct {
		name string
		fn   func()
	}{
		{"GetPRIterations", func() { _, _ = cp.GetPRIterations("X", "r", 1) }},
		{"GetPRIterationChanges", func() { _, _ = cp.GetPRIterationChanges("X", "r", 1, 1) }},
		{"VotePullRequest", func() { _ = cp.VotePullRequest("X", "r", 1, 10) }},
		{"GetFileContent", func() { _, _ = cp.GetFileContent("X", "r", "f", "main") }},
		{"AddPRCodeComment", func() { _, _ = cp.AddPRCodeComment("X", "r", 1, "f", 1, "c") }},
		{"AddPRComment", func() { _, _ = cp.AddPRComment("X", "r", 1, "c") }},
		{"ReplyToThread", func() { _, _ = cp.ReplyToThread("X", "r", 1, 1, "c") }},
		{"UpdateThreadStatus", func() { _ = cp.UpdateThreadStatus("X", "r", 1, 1, "Fixed") }},
		{"GetWorkItemTypeStates", func() { _, _ = cp.GetWorkItemTypeStates("X", "Bug") }},
		{"UpdateWorkItemState", func() { _ = cp.UpdateWorkItemState("X", 1, "Active") }},
		{"GetWorkItemComments", func() { _, _ = cp.GetWorkItemComments("X", 1) }},
		{"AddWorkItemComment", func() { _, _ = cp.AddWorkItemComment("X", 1, "t") }},
		{"GetBuildTimeline", func() { _, _ = cp.GetBuildTimeline("X", 1) }},
		{"GetBuildLogContent", func() { _, _ = cp.GetBuildLogContent("X", 1, 1) }},
		{"PRThreadWebURL", func() { _ = cp.PRThreadWebURL("X", "r", 1, 1) }},
		{"PipelineURL", func() { _ = cp.PipelineURL("X", 1) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a.lastRouteScope = ""
			tc.fn()
			if a.lastRouteScope != "X" {
				t.Errorf("want scope=%q forwarded to backend, got %q", "X", a.lastRouteScope)
			}
		})
	}
}

// TestCompositeProvider_ListMyWorkItems verifies fan-out of ListMyWorkItems.
func TestCompositeProvider_ListMyWorkItems(t *testing.T) {
	a := &fakeBackend{
		kind:    provider.KindAzure,
		scopes:  []string{"A"},
		myItems: []provider.WorkItem{makeItem("A", t2)},
	}
	b := &fakeBackend{
		kind:    provider.KindGitHub,
		scopes:  []string{"B"},
		myItems: []provider.WorkItem{makeItem("B", t3)},
	}
	cp := provider.NewCompositeProvider(a, b)

	items, err := cp.ListMyWorkItems(10, provider.ListOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2, got %d", len(items))
	}
	if !items[0].ChangedDate.Equal(t3) {
		t.Errorf("want first at t3, got %v", items[0].ChangedDate)
	}
}
