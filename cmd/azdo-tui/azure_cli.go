package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hari-incoming/hazdo/internal/azdevops"
	"github.com/hari-incoming/hazdo/internal/config"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

func runAzureCLI(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected 'pipeline list', 'pipeline run', or 'pr create'")
	}

	switch args[0] {
	case "pipeline":
		if len(args) < 2 {
			return fmt.Errorf("usage: azdo pipeline <list|run> [options]")
		}
		switch args[1] {
		case "list":
			return runPipelineList(args[2:])
		case "run":
			return runPipeline(args[2:])
		default:
			return fmt.Errorf("unknown pipeline command %q; use 'list' or 'run'", args[1])
		}
	case "pr":
		if len(args) < 2 || args[1] != "create" {
			return fmt.Errorf("usage: azdo pr create [options]")
		}
		return runPRCreate(args[2:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func azureClient(project string) (*azdevops.MultiClient, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", fmt.Errorf("load azdo config: %w", err)
	}
	if !cfg.HasAzure() {
		return nil, "", fmt.Errorf("Azure DevOps is not configured; add organization and projects to config.yaml")
	}

	project, err = selectProject(cfg, project)
	if err != nil {
		return nil, "", err
	}

	pat, err := config.NewKeyringStore().GetPAT()
	if err != nil {
		return nil, "", fmt.Errorf("get Azure DevOps PAT: %w", err)
	}
	mc, err := azdevops.NewMultiClient(cfg.Organization, cfg.Projects, pat, cfg.DisplayNames)
	if err != nil {
		return nil, "", fmt.Errorf("create Azure DevOps client: %w", err)
	}
	return mc, project, nil
}

func selectProject(cfg *config.Config, project string) (string, error) {
	if project != "" {
		for _, configured := range cfg.Projects {
			if configured == project {
				return project, nil
			}
		}
		return "", fmt.Errorf("project %q is not configured; configured projects: %s", project, strings.Join(cfg.Projects, ", "))
	}
	if len(cfg.Projects) != 1 {
		return "", fmt.Errorf("choose a project with --project (configured: %s)", strings.Join(cfg.Projects, ", "))
	}
	return cfg.Projects[0], nil
}

func runPipelineList(args []string) error {
	flags := newFlags("pipeline list")
	project := flags.String("project", "", "Azure DevOps project (required when multiple projects are configured)")
	search := flags.String("search", "", "case-insensitive substring to match pipeline names")
	helpRequested, err := parseCommandFlags(flags, args)
	if err != nil {
		return err
	}
	if helpRequested {
		return nil
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	mc, selectedProject, err := azureClient(*project)
	if err != nil {
		return err
	}
	definitions, err := mc.ClientFor(selectedProject).ListPipelineDefinitions()
	if err != nil {
		return err
	}

	query := strings.ToLower(strings.TrimSpace(*search))
	fmt.Printf("Project: %s\nID\tNAME\tPATH\n", selectedProject)
	matches := 0
	for _, definition := range definitions {
		if query != "" && !strings.Contains(strings.ToLower(definition.Name), query) &&
			!strings.Contains(strings.ToLower(definition.Path), query) {
			continue
		}
		fmt.Printf("%d\t%s\t%s\n", definition.ID, definition.Name, definition.Path)
		matches++
	}
	if matches == 0 {
		fmt.Println("No pipeline definitions found.")
	}
	return nil
}

func runPipeline(args []string) error {
	flags := newFlags("pipeline run")
	project := flags.String("project", "", "Azure DevOps project (required when multiple projects are configured)")
	branch := flags.String("branch", "", "branch to run (defaults to the current Git branch)")
	helpRequested, err := parseCommandFlags(flags, args)
	if err != nil {
		return err
	}
	if helpRequested {
		return nil
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: azdo pipeline run <pipeline-name-or-id> [--project PROJECT] [--branch BRANCH]")
	}

	mc, selectedProject, err := azureClient(*project)
	if err != nil {
		return err
	}
	client := mc.ClientFor(selectedProject)
	definitions, err := client.ListPipelineDefinitions()
	if err != nil {
		return err
	}
	definition, err := findPipelineDefinition(definitions, flags.Arg(0))
	if err != nil {
		return err
	}

	runBranch := strings.TrimSpace(*branch)
	if runBranch == "" {
		runBranch, err = currentGitBranch()
		if err != nil {
			return fmt.Errorf("set --branch or run this command inside a Git repository: %w", err)
		}
	}
	run, err := client.QueuePipeline(definition.ID, runBranch)
	if err != nil {
		return err
	}

	fmt.Printf("Queued %s (pipeline %d) on %s; run %d\n", definition.Name, definition.ID, runBranch, run.ID)
	if run.Links.Web.Href != "" {
		fmt.Println(run.Links.Web.Href)
	}
	return nil
}

func findPipelineDefinition(definitions []azdevops.PipelineDefinition, value string) (azdevops.PipelineDefinition, error) {
	if id, err := strconv.Atoi(value); err == nil {
		for _, definition := range definitions {
			if definition.ID == id {
				return definition, nil
			}
		}
		return azdevops.PipelineDefinition{}, fmt.Errorf("pipeline definition ID %d was not found", id)
	}

	var matches []azdevops.PipelineDefinition
	for _, definition := range definitions {
		if strings.EqualFold(definition.Name, value) {
			matches = append(matches, definition)
		}
	}
	switch len(matches) {
	case 0:
		return azdevops.PipelineDefinition{}, fmt.Errorf("pipeline %q was not found; run 'azdo pipeline list --search %q'", value, value)
	case 1:
		return matches[0], nil
	default:
		var ids []string
		for _, definition := range matches {
			ids = append(ids, strconv.Itoa(definition.ID))
		}
		return azdevops.PipelineDefinition{}, fmt.Errorf("pipeline name %q is ambiguous; use one of these IDs: %s", value, strings.Join(ids, ", "))
	}
}

func runPRCreate(args []string) error {
	flags := newFlags("pr create")
	project := flags.String("project", "", "Azure DevOps project (required when multiple projects are configured)")
	repository := flags.String("repo", "", "Azure repository name or ID")
	title := flags.String("title", "", "pull request title")
	description := flags.String("description", "", "pull request description")
	source := flags.String("source", "", "source branch (defaults to the current Git branch)")
	target := flags.String("target", "", "target branch (defaults to main)")
	draft := flags.Bool("draft", false, "create as a draft pull request")
	interactive := flags.Bool("interactive", false, "choose project, repository, branches, and PR message interactively")
	helpRequested, err := parseCommandFlags(flags, args)
	if err != nil {
		return err
	}
	if helpRequested {
		return nil
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if !*interactive && strings.TrimSpace(*repository) == "" {
		return fmt.Errorf("--repo is required")
	}
	if !*interactive && strings.TrimSpace(*title) == "" {
		return fmt.Errorf("--title is required")
	}

	var promptReader *bufio.Reader
	if *interactive {
		promptReader = bufio.NewReader(os.Stdin)
		if strings.TrimSpace(*project) == "" {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load azdo config: %w", err)
			}
			if !cfg.HasAzure() {
				return fmt.Errorf("Azure DevOps is not configured; add organization and projects to config.yaml")
			}
			if len(cfg.Projects) > 1 {
				*project, err = promptChoice(promptReader, os.Stdout, "Choose an Azure DevOps project", cfg.Projects, "")
				if err != nil {
					return err
				}
			}
		}
	}

	mc, selectedProject, err := azureClient(*project)
	if err != nil {
		return err
	}
	client := mc.ClientFor(selectedProject)
	repositories, err := client.ListRepositories()
	if err != nil {
		return err
	}

	if *interactive {
		return runInteractivePRCreate(promptReader, os.Stdout, client, mc.GetOrg(), selectedProject,
			repositories, *repository, *source, *target, *title, *description, *draft)
	}

	repo, err := findRepository(repositories, *repository)
	if err != nil {
		return err
	}
	sourceBranch := strings.TrimSpace(*source)
	if sourceBranch == "" {
		sourceBranch, err = currentGitBranch()
		if err != nil {
			return fmt.Errorf("set --source or run this command inside a Git repository: %w", err)
		}
	}
	targetBranch := strings.TrimSpace(*target)
	if targetBranch == "" {
		targetBranch = "main"
	}
	pr, err := client.CreatePullRequest(repo.ID, *title, *description, sourceBranch, targetBranch, *draft)
	if err != nil {
		return err
	}
	fmt.Printf("Created PR %d: %s (%s -> %s)\n", pr.ID, pr.Title, sourceBranch, targetBranch)
	fmt.Printf("https://dev.azure.com/%s/%s/_git/%s/pullrequest/%d\n",
		mc.GetOrg(), selectedProject, repo.ID, pr.ID)
	return nil
}

func runInteractivePRCreate(
	reader *bufio.Reader,
	writer io.Writer,
	client *azdevops.Client,
	organization, project string,
	repositories []azdevops.Repository,
	repositoryValue, sourceValue, targetValue, titleValue, descriptionValue string,
	draft bool,
) error {
	var repo azdevops.Repository
	var err error
	if strings.TrimSpace(repositoryValue) != "" {
		repo, err = findRepository(repositories, repositoryValue)
		if err != nil {
			return err
		}
	} else {
		options := make([]string, len(repositories))
		for i, item := range repositories {
			options[i] = fmt.Sprintf("%s (%s)", item.Name, item.ID)
		}
		selected, err := promptChoice(reader, writer, "Choose a repository", options, "")
		if err != nil {
			return err
		}
		for i, option := range options {
			if option == selected {
				repo = repositories[i]
				break
			}
		}
		if repo.ID == "" {
			return fmt.Errorf("selected repository is unavailable")
		}
	}

	branches, err := client.ListBranches(repo.ID)
	if err != nil {
		return err
	}
	if len(branches) == 0 {
		return fmt.Errorf("repository %q has no branches", repo.Name)
	}

	sourceDefault := matchingBranch(branches, sourceValue)
	if sourceDefault == "" && strings.TrimSpace(sourceValue) == "" {
		if currentBranch, branchErr := currentGitBranch(); branchErr == nil {
			sourceDefault = matchingBranch(branches, currentBranch)
		}
	}
	sourceBranch, err := choosePRBranch(reader, writer, "Choose the source branch", branches, sourceValue, sourceDefault)
	if err != nil {
		return err
	}

	targetDefault := matchingBranch(branches, targetValue)
	if targetDefault == "" && strings.TrimSpace(targetValue) == "" {
		targetDefault = matchingBranch(branches, repo.DefaultBranch)
		if targetDefault == "" {
			targetDefault = matchingBranch(branches, "main")
		}
	}
	targetBranch, err := choosePRBranch(reader, writer, "Choose the target branch", branches, targetValue, targetDefault)
	if err != nil {
		return err
	}
	if strings.EqualFold(sourceBranch, targetBranch) {
		return fmt.Errorf("source and target branches must be different")
	}

	title, err := promptText(reader, writer, "Pull request title", titleValue, true)
	if err != nil {
		return err
	}
	fmt.Fprintf(writer, "\nCreate pull request in %s/%s?\n", project, repo.Name)
	fmt.Fprintf(writer, "  Source:      %s\n  Target:      %s\n  Title:       %s\n", sourceBranch, targetBranch, title)
	if draft {
		fmt.Fprintln(writer, "  Draft:       yes")
	}
	description, readyToReview, err := promptMultilineDescription(writer, descriptionValue)
	if err != nil {
		return err
	}
	if !readyToReview {
		fmt.Fprintln(writer, "Pull request creation cancelled.")
		return nil
	}

	printPRSummary(writer, project, repo.Name, sourceBranch, targetBranch, title, description, draft)
	confirmed, err := promptConfirmation(reader, writer, "Submit this pull request? [y/N]")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Fprintln(writer, "Pull request creation cancelled.")
		return nil
	}

	pr, err := client.CreatePullRequest(repo.ID, title, description, sourceBranch, targetBranch, draft)
	if err != nil {
		return err
	}
	fmt.Fprintf(writer, "Created PR %d: %s (%s -> %s)\n", pr.ID, pr.Title, sourceBranch, targetBranch)
	fmt.Fprintf(writer, "https://dev.azure.com/%s/%s/_git/%s/pullrequest/%d\n",
		organization, project, repo.ID, pr.ID)
	return nil
}

func printPRSummary(writer io.Writer, project, repository, sourceBranch, targetBranch, title, description string, draft bool) {
	fmt.Fprintf(writer, "\nReview pull request in %s:\n", project)
	fmt.Fprintf(writer, "  Repository:  %s\n", repository)
	fmt.Fprintf(writer, "  Source:      %s\n", sourceBranch)
	fmt.Fprintf(writer, "  Target:      %s\n", targetBranch)
	fmt.Fprintf(writer, "  Title:       %s\n", title)
	if description == "" {
		fmt.Fprintln(writer, "  Description: (none)")
	} else {
		fmt.Fprintln(writer, "  Description:")
		for _, line := range strings.Split(description, "\n") {
			fmt.Fprintf(writer, "    %s\n", line)
		}
	}
	if draft {
		fmt.Fprintln(writer, "  Draft:       yes")
	}
}

type prDescriptionModel struct {
	textarea      textarea.Model
	readyToReview bool
	focusCmd      tea.Cmd
}

func newPRDescriptionModel(initial string) prDescriptionModel {
	input := textarea.New()
	input.Placeholder = "Optional description..."
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetHeight(8)
	input.SetValue(initial)
	focusCmd := input.Focus()
	return prDescriptionModel{textarea: input, focusCmd: focusCmd}
}

func (m prDescriptionModel) Init() tea.Cmd {
	return m.focusCmd
}

func (m prDescriptionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			return m, tea.Quit
		}
		if key.Type == tea.KeyCtrlJ || (key.Type == tea.KeyEnter && key.Alt) {
			m.readyToReview = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m prDescriptionModel) View() string {
	return "Pull request description (optional)\n" +
		"Enter: new line • Ctrl+J/Alt+Enter: review • Esc: cancel\n\n" +
		m.textarea.View()
}

func promptMultilineDescription(writer io.Writer, initial string) (string, bool, error) {
	model := newPRDescriptionModel(initial)
	program := tea.NewProgram(model, tea.WithOutput(writer), tea.WithoutSignalHandler())
	result, err := program.Run()
	if err != nil {
		return "", false, fmt.Errorf("read pull request description: %w", err)
	}
	final, ok := result.(prDescriptionModel)
	if !ok {
		return "", false, fmt.Errorf("pull request description editor returned an unexpected model")
	}
	return final.textarea.Value(), final.readyToReview, nil
}

func choosePRBranch(reader *bufio.Reader, writer io.Writer, label string, branches []string, requested, defaultBranch string) (string, error) {
	if strings.TrimSpace(requested) != "" {
		if branch := matchingBranch(branches, requested); branch != "" {
			return branch, nil
		}
		return "", fmt.Errorf("branch %q was not found in the selected repository", requested)
	}
	return promptChoice(reader, writer, label, branches, defaultBranch)
}

func matchingBranch(branches []string, candidate string) string {
	candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "refs/heads/")
	for _, branch := range branches {
		if strings.EqualFold(branch, candidate) {
			return branch
		}
	}
	return ""
}

func promptChoice(reader *bufio.Reader, writer io.Writer, label string, options []string, defaultValue string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no choices available for %s", strings.ToLower(label))
	}
	defaultIndex := -1
	for i, option := range options {
		if strings.EqualFold(option, defaultValue) {
			defaultIndex = i
			break
		}
	}

	fmt.Fprintln(writer, label+":")
	for i, option := range options {
		fmt.Fprintf(writer, "  %d) %s\n", i+1, option)
	}
	if defaultIndex >= 0 {
		fmt.Fprintf(writer, "Selection [%d]: ", defaultIndex+1)
	} else {
		fmt.Fprint(writer, "Selection: ")
	}
	value, err := readPromptLine(reader)
	if err != nil {
		return "", err
	}
	if value == "" && defaultIndex >= 0 {
		return options[defaultIndex], nil
	}
	for _, option := range options {
		if strings.EqualFold(value, option) {
			return option, nil
		}
	}
	index, err := strconv.Atoi(value)
	if err != nil || index < 1 || index > len(options) {
		return "", fmt.Errorf("select a valid number from 1 to %d", len(options))
	}
	return options[index-1], nil
}

func promptText(reader *bufio.Reader, writer io.Writer, label, defaultValue string, required bool) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(writer, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(writer, "%s: ", label)
	}
	value, err := readPromptLine(reader)
	if err != nil {
		return "", err
	}
	if value == "" {
		value = strings.TrimSpace(defaultValue)
	}
	if required && value == "" {
		return "", fmt.Errorf("%s is required", strings.ToLower(label))
	}
	return value, nil
}

func promptConfirmation(reader *bufio.Reader, writer io.Writer, label string) (bool, error) {
	fmt.Fprint(writer, label+" ")
	value, err := readPromptLine(reader)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(value) {
	case "y", "yes":
		return true, nil
	case "", "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("answer yes or no")
	}
}

func readPromptLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("read interactive input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func findRepository(repositories []azdevops.Repository, value string) (azdevops.Repository, error) {
	var matches []azdevops.Repository
	for _, repository := range repositories {
		if repository.ID == value || strings.EqualFold(repository.Name, value) {
			matches = append(matches, repository)
		}
	}
	switch len(matches) {
	case 0:
		return azdevops.Repository{}, fmt.Errorf("repository %q was not found in this project", value)
	case 1:
		return matches[0], nil
	default:
		return azdevops.Repository{}, fmt.Errorf("repository name %q is ambiguous; use its ID", value)
	}
}

func currentGitBranch() (string, error) {
	command := exec.Command("git", "branch", "--show-current")
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("git branch lookup failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	branch := strings.TrimSpace(string(output))
	if branch == "" {
		return "", fmt.Errorf("the current Git checkout has no branch (detached HEAD)")
	}
	return branch, nil
}

func newFlags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: azdo %s [options]\n", name)
		flags.PrintDefaults()
	}
	return flags
}

func parseCommandFlags(flags *flag.FlagSet, args []string) (bool, error) {
	err := flags.Parse(interleaveFlagArguments(flags, args))
	if errors.Is(err, flag.ErrHelp) {
		return true, nil
	}
	return false, err
}

func interleaveFlagArguments(flags *flag.FlagSet, args []string) []string {
	flagArguments := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	values := make(map[string]bool)
	flags.VisitAll(func(item *flag.Flag) {
		_, isBoolean := item.Value.(interface{ IsBoolFlag() bool })
		values[item.Name] = !isBoolean
	})

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}

		name := strings.TrimLeft(arg, "-")
		name, _, hasValue := strings.Cut(name, "=")
		flagArguments = append(flagArguments, arg)
		if values[name] && !hasValue && i+1 < len(args) {
			i++
			flagArguments = append(flagArguments, args[i])
		}
	}
	return append(flagArguments, positionals...)
}

func writeCommandHelp(w io.Writer) {
	fmt.Fprintln(w, `Azure DevOps commands:
  azdo pipeline list [--project PROJECT] [--search TEXT]
  azdo pipeline run <pipeline-name-or-id> [--project PROJECT] [--branch BRANCH]
  azdo pr create --repo REPOSITORY --title TITLE [--project PROJECT]
                 [--source BRANCH] [--target BRANCH] [--description TEXT] [--draft]
  azdo pr create --interactive [--project PROJECT] [--repo REPOSITORY] [--draft]

Pipeline names and definitions include pipelines with no recent or running builds.
When --branch/--source is omitted, the current Git branch is used.
Interactive PR creation lists repository branches, prompts for both branches and the
title, then opens a multiline description editor. Press Enter for a new line and
Ctrl+Enter to review the PR summary. Confirm with y/yes to submit or press Enter
to cancel; Esc cancels while editing the description.`)
}
