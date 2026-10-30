package main

import (
	"bufio"
	"strings"
	"testing"

	"github.com/hari-incoming/hazdo/internal/azdevops"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPRDescriptionModel_EnterAddsLineAndCtrlJOpensReview(t *testing.T) {
	model := newPRDescriptionModel("First line")
	model.Init()

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(prDescriptionModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Second line")})
	model = updated.(prDescriptionModel)
	if model.readyToReview {
		t.Fatal("regular Enter unexpectedly opened the review")
	}
	if got, want := model.textarea.Value(), "First line\nSecond line"; got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	model = updated.(prDescriptionModel)
	if !model.readyToReview {
		t.Fatal("Ctrl+J did not open the review")
	}
	if cmd == nil {
		t.Fatal("Ctrl+J did not quit the description editor")
	}
}

func TestPRDescriptionModel_AltEnterOpensReview(t *testing.T) {
	model := newPRDescriptionModel("Description")
	model.Init()

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	model = updated.(prDescriptionModel)
	if !model.readyToReview {
		t.Fatal("Alt+Enter did not open the review")
	}
	if cmd == nil {
		t.Fatal("Alt+Enter did not quit the description editor")
	}
}

func TestPromptConfirmationRequiresExplicitYes(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{input: "yes\n", want: true},
		{input: "y\n", want: true},
		{input: "no\n", want: false},
		{input: "\n", want: false},
	} {
		var output strings.Builder
		got, err := promptConfirmation(bufio.NewReader(strings.NewReader(tc.input)), &output, "Submit?")
		if err != nil {
			t.Fatalf("promptConfirmation(%q) error = %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("promptConfirmation(%q) = %t, want %t", tc.input, got, tc.want)
		}
	}
}

func TestPrintPRSummaryIncludesRequestDetailsAndMultilineDescription(t *testing.T) {
	var output strings.Builder
	printPRSummary(&output, "project", "repository", "feature/source", "main", "Add capability", "First line\nSecond line", false)

	for _, want := range []string{
		"Repository:  repository",
		"Source:      feature/source",
		"Target:      main",
		"Title:       Add capability",
		"First line\n    Second line",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("summary missing %q:\n%s", want, output.String())
		}
	}
}

func TestFindPipelineDefinition(t *testing.T) {
	definitions := []azdevops.PipelineDefinition{
		{ID: 12, Name: "CI Build"},
		{ID: 34, Name: "ci build"},
		{ID: 56, Name: "Deploy"},
	}

	got, err := findPipelineDefinition(definitions, "56")
	if err != nil || got.Name != "Deploy" {
		t.Fatalf("ID lookup = %+v, %v", got, err)
	}
	if _, err := findPipelineDefinition(definitions, "CI Build"); err == nil {
		t.Fatal("expected duplicate pipeline name to be reported as ambiguous")
	}

	got, err = findPipelineDefinition(definitions, "Deploy")
	if err != nil || got.ID != 56 {
		t.Fatalf("name lookup = %+v, %v", got, err)
	}
}

func TestFindRepository(t *testing.T) {
	repositories := []azdevops.Repository{
		{ID: "repo-1", Name: "Hub"},
		{ID: "repo-2", Name: "Tools"},
	}

	got, err := findRepository(repositories, "hub")
	if err != nil || got.ID != "repo-1" {
		t.Fatalf("name lookup = %+v, %v", got, err)
	}
	got, err = findRepository(repositories, "repo-2")
	if err != nil || got.Name != "Tools" {
		t.Fatalf("ID lookup = %+v, %v", got, err)
	}
}

func TestPromptChoice(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		defaultValue string
		want         string
		wantErr      bool
	}{
		{name: "default selection", input: "\n", defaultValue: "main", want: "main"},
		{name: "numbered selection", input: "2\n", defaultValue: "main", want: "develop"},
		{name: "named branch selection", input: "feature/test\n", want: "feature/test"},
		{name: "invalid selection", input: "8\n", wantErr: true},
	}
	options := []string{"main", "develop", "feature/test"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			got, err := promptChoice(bufio.NewReader(strings.NewReader(tt.input)), &output, "Choose branch", options, tt.defaultValue)
			if (err != nil) != tt.wantErr {
				t.Fatalf("promptChoice() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("promptChoice() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChoosePRBranchValidatesAgainstRepositoryBranches(t *testing.T) {
	branches := []string{"develop", "feature/test", "main"}
	got, err := choosePRBranch(bufio.NewReader(strings.NewReader("")), &strings.Builder{},
		"Choose source branch", branches, "refs/heads/feature/test", "")
	if err != nil || got != "feature/test" {
		t.Fatalf("choosePRBranch() = %q, %v; want feature/test", got, err)
	}
	if _, err := choosePRBranch(bufio.NewReader(strings.NewReader("")), &strings.Builder{},
		"Choose source branch", branches, "missing", "main"); err == nil {
		t.Fatal("expected unknown branch to be rejected")
	}
}

func TestParseCommandFlagsAllowsOptionsAfterPositionals(t *testing.T) {
	flags := newFlags("pipeline run")
	branch := flags.String("branch", "", "branch")
	draft := flags.Bool("draft", false, "draft")

	helpRequested, err := parseCommandFlags(flags, []string{"CI Build", "--branch", "feature/work", "--draft"})
	if err != nil || helpRequested {
		t.Fatalf("parseCommandFlags() = %v, %v", helpRequested, err)
	}
	if flags.Arg(0) != "CI Build" || *branch != "feature/work" || !*draft {
		t.Fatalf("parsed positional=%q branch=%q draft=%t", flags.Arg(0), *branch, *draft)
	}
}
