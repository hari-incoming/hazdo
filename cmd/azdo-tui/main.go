package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/hari-incoming/hazdo/internal/app"
	"github.com/hari-incoming/hazdo/internal/azdevops"
	"github.com/hari-incoming/hazdo/internal/cli"
	"github.com/hari-incoming/hazdo/internal/config"
	"github.com/hari-incoming/hazdo/internal/demo"
	"github.com/hari-incoming/hazdo/internal/github"
	"github.com/hari-incoming/hazdo/internal/provider"
	"github.com/hari-incoming/hazdo/internal/state"
	"github.com/hari-incoming/hazdo/internal/ui/components"
	"github.com/hari-incoming/hazdo/internal/ui/patinput"
	"github.com/hari-incoming/hazdo/internal/ui/providerselect"
	"github.com/hari-incoming/hazdo/internal/ui/setupwizard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Build-time variables injected via ldflags by goreleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	action := cli.ParseArgs(args)

	switch action {
	case cli.ActionHelp:
		return runHelp()
	case cli.ActionVersion:
		return runVersion()
	case cli.ActionAuth:
		return runAuth()
	case cli.ActionConfig:
		return runConfigure(args[2:])
	case cli.ActionDemo:
		return demo.Run(version, commit)
	case cli.ActionAzureCLI:
		return runAzureCLI(args[1:])
	default:
		return runTUI()
	}
}

func runHelp() error {
	configPath, _ := config.GetPath()
	if configPath == "" {
		configPath = "~/.config/azdo-tui/config.yaml"
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99"))
	fmt.Println(titleStyle.Render(strings.Join(components.LogoArt, "\n")))

	fmt.Printf(`Hazdo - A TUI for Azure DevOps and GitHub (command: azdo, %s)

Usage:
  azdo              Start the TUI application
  azdo auth         Set or update credentials for Azure DevOps (PAT) or GitHub
  azdo config --organization <name>
                   Update the configured Azure DevOps organization
  azdo demo         Launch with mock data (for screenshots/demos)
  azdo pipeline ... Azure pipeline list/search/run commands
  azdo pr create ... Create an Azure DevOps pull request
  azdo pr create --interactive
                     Choose project, repository, branches, and PR message, then submit
  azdo --help       Show this help message
  azdo --version    Show version information

Configuration:
  Config file:     %s
  Token storage:   System keyring / credential manager (service: azdo-tui)
  Azure env:       AZDO_PAT (non-interactive use)
  GitHub env:      GITHUB_TOKEN (non-interactive use)

Required Azure DevOps PAT scopes:
  Build        (Read & Execute) - list and queue pipelines, build logs
  Pipeline Resources (Use) - approve pending protected-resource requests
  Code         (Read & Write) - pull requests, voting, comments
  Work Items   (Read & Write) - queries, comments, state changes

Required GitHub token scopes:
  Classic PAT:   repo          (private repos) or public_repo (public only)
                 notifications (Notifications tab: list, mark read, mark done)
  Fine-grained:  Metadata      (read)
                 Contents      (read)
                 Issues        (read & write)
                 Pull requests (read & write)
                 Actions       (read)
  Note: resolving PR comment threads requires a classic 'repo' PAT;
        fine-grained tokens are commonly rejected for that operation.
  Note: the GitHub share of the Notifications tab requires a CLASSIC
        token. GitHub's notifications API supports no fine-grained
        permission, so there is nothing to grant a fine-grained token
        here. The Azure DevOps share of the tab is unaffected.

Keyboard shortcuts (in TUI):
  Navigation:
    ↑/k          Move up
    ↓/j          Move down
    gg / G       Jump to first / last row
    pgup/pgdn    Page up / down
    enter        View details / expand
    esc          Go back

  Tabs:
    1-6          Switch tabs (All Pipelines, Active Pipelines, Pull Requests,
                 Work Items, Notifications, Metrics when Azure is configured)
    ←/→          Previous / next tab
    h/l          Previous / next tab

  Actions:
    f or /       Search / filter
    n            Create PR (Azure PR list) / run again (recent pipeline detail)
    m            Toggle my items (PRs / work items)
    A            Toggle as reviewer (PRs)
    T            Filter by tag (work items)
    r            Refresh data
    v            Vote on PR (detail view)
    s            Change work item state (detail view)
    c            Add comment (work item detail)
    o            Open in browser (PR / work item / pipeline detail)
    t            Select theme
    ?            Toggle help overlay
    q            Quit

  Text Selection:
    Double-click a word to select it, then use your terminal's copy shortcut
    (Ctrl+Shift+C in Windows Terminal). Mouse clicks and wheel controls are
    passed through to the terminal; use keyboard shortcuts to navigate the TUI.

  Code Review (PR diff):
    c            Create new comment
    p            Reply to nearest thread
    x            Resolve nearest thread
    n / N        Jump to next / previous comment

  Pipeline Detail:
    zM / zR      Collapse / expand all pipeline details
    n            Run this pipeline again; copy prior parameters (recent-run detail)
    zP           View parameters (pipeline or recent-run detail)
    p            View pending resource approvals; a approves selected request (Azure)

  Log Viewer (pipelines):
    /            Search log output
    n / N        Go to next / previous search match
    g            Go to top
    G            Go to bottom

For more information, visit: https://github.com/hari-incoming/hazdo
`, version, configPath)
	writeCommandHelp(os.Stdout)

	return nil
}

func runVersion() error {
	fmt.Printf("Hazdo version %s (commit: %s, built: %s)\n", version, commit, date)
	return nil
}

func runAuth() error {
	store := config.NewKeyringStore()

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99"))
	fmt.Println(titleStyle.Render(strings.Join(components.LogoArt, "\n")))
	fmt.Println()

	// Prompt the user to pick a provider.
	selModel := providerselect.NewModel()
	selProg := tea.NewProgram(selModel)
	selResult, err := selProg.Run()
	if err != nil {
		return fmt.Errorf("failed to run provider selection: %w", err)
	}
	finalSel, ok := selResult.(providerselect.Model)
	if !ok {
		return fmt.Errorf("unexpected model type from provider selection")
	}
	if finalSel.Cancelled() {
		return nil
	}

	switch finalSel.Selected() {
	case providerselect.ProviderAzure:
		return runAuthAzure(store)
	case providerselect.ProviderGitHub:
		return runAuthGitHub(store)
	default:
		return fmt.Errorf("unknown provider selected")
	}
}

func keyringUnavailableError(err error) error {
	switch runtime.GOOS {
	case "linux":
		return fmt.Errorf("system keyring unavailable: %w; install and unlock a Secret Service provider such as GNOME Keyring or KWallet, then retry", err)
	case "windows":
		return fmt.Errorf("Windows Credential Manager unavailable: %w; ensure the Windows credential service is enabled, then retry", err)
	case "darwin":
		return fmt.Errorf("macOS Keychain unavailable: %w; unlock your login keychain, then retry", err)
	default:
		return fmt.Errorf("system keyring unavailable: %w; enable a supported credential manager, then retry", err)
	}
}

func requireSystemKeyring(store *config.KeyringStore) error {
	if err := store.CheckKeyring(); err != nil {
		return keyringUnavailableError(err)
	}
	return nil
}

func runConfigure(args []string) error {
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: azdo config --organization <name>")
		flags.PrintDefaults()
	}
	organization := flags.String("organization", "", "Azure DevOps organization name")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(*organization) == "" {
		return fmt.Errorf("organization is required; usage: azdo config --organization <name>")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	if err := updateOrganization(cfg, *organization); err != nil {
		return err
	}

	fmt.Printf("Azure DevOps organization updated to %q.\n", cfg.Organization)
	return nil
}

func updateOrganization(cfg *config.Config, organization string) error {
	organization = strings.TrimSpace(organization)
	if organization == "" {
		return fmt.Errorf("organization cannot be empty")
	}

	cfg.Organization = organization
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}
	return nil
}

// runAuthAzure handles Azure DevOps PAT setup.
func runAuthAzure(store *config.KeyringStore) error {
	_, err := store.GetPAT()
	isUpdate := err == nil

	if isUpdate {
		fmt.Println("Azure DevOps PAT Update")
		fmt.Println("This will replace your existing Personal Access Token.")
	} else {
		fmt.Println("Azure DevOps PAT Setup")
		fmt.Println("This will store your Personal Access Token.")
	}
	fmt.Println()
	fmt.Println(patinput.PermissionInfoPlain())
	fmt.Println()

	pat, err := promptForPATWithMode(store, isUpdate)
	if err != nil {
		return fmt.Errorf("failed to set PAT: %w", err)
	}

	if pat != "" {
		fmt.Printf("\nPAT saved successfully to %s.\n", store.PATStorageDescription())
	}

	return nil
}

// runAuthGitHub is the GitHub token auth flow.
func runAuthGitHub(store *config.KeyringStore) error {
	if err := requireSystemKeyring(store); err != nil {
		return err
	}

	_, err := store.GetGitHubToken()
	isUpdate := err == nil

	if isUpdate {
		fmt.Println("GitHub Token Update")
		fmt.Println("This will replace your existing GitHub token in the system keyring (service: azdo-tui).")
	} else {
		fmt.Println("GitHub Token Setup")
		fmt.Println("This will store your GitHub token in the system keyring (service: azdo-tui).")
		fmt.Println("Tip: GITHUB_TOKEN environment variable is also accepted as a fallback.")
	}
	fmt.Println()
	fmt.Println(`Required token scopes:
  Classic PAT:  repo            (private repos) or public_repo (public only)
                notifications   (Notifications tab: list, mark read, mark done)
  Fine-grained: Metadata        (read)
                Contents        (read)
                Issues          (read & write)
                Pull requests   (read & write)
                Actions         (read)
  Note: the GitHub share of the Notifications tab requires a CLASSIC token —
  GitHub's notifications API supports no fine-grained permission. The Azure
  DevOps share of the tab is unaffected.`)
	fmt.Println()

	var model patinput.Model
	if isUpdate {
		model = patinput.NewGitHubModelForUpdate()
	} else {
		model = patinput.NewGitHubModel()
	}
	p := tea.NewProgram(model)

	m, err := p.Run()
	if err != nil {
		return fmt.Errorf("failed to run GitHub token input: %w", err)
	}

	finalModel, ok := m.(patinput.Model)
	if !ok {
		return fmt.Errorf("unexpected model type from GitHub token input")
	}

	token := finalModel.GetPAT()
	if token == "" {
		return nil
	}

	if err := store.SetGitHubToken(token); err != nil {
		return fmt.Errorf("failed to save GitHub token: %w", err)
	}

	fmt.Println("\nGitHub token saved successfully to system keyring.")
	return nil
}

func runTUI() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			cfg, err = runSetupWizard()
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Build the configured backends and assemble a CompositeProvider.
	store := config.NewKeyringStore()

	var backends []provider.Provider
	var azureMC *azdevops.MultiClient
	var azureNotifStore *azdevops.TriageStore

	// --- Azure backend (only when fully configured) ---
	if cfg.HasAzure() {
		pat, err := store.GetPAT()
		if err != nil {
			if errors.Is(err, config.ErrNotFound) {
				pat, err = promptForPAT(store)
				if err != nil {
					return fmt.Errorf("failed to set PAT: %w", err)
				}
			} else if keyringErr := store.CheckKeyring(); keyringErr != nil {
				return keyringUnavailableError(keyringErr)
			} else {
				return fmt.Errorf("failed to get PAT: %w", err)
			}
		}

		client, err := azdevops.NewMultiClient(cfg.Organization, cfg.Projects, pat, cfg.DisplayNames)
		if err != nil {
			return fmt.Errorf("failed to create Azure DevOps client: %w", err)
		}
		azureMC = client

		// NewAdapter alone (bare, no notifications store) still satisfies
		// provider.NotificationSource by method set — Go's structural typing
		// does not care that its notifStore field is left at its zero value.
		// That would make the Notifications tab appear for an Azure-only
		// config while List's nil-store guard makes every poll fail
		// (azdevops.Adapter.list's own doc comment, task 8 review, 🔴
		// finding 1). NewAdapterWithNotifications is what actually wires the
		// local read/done triage store notifications need.
		notifPath, err := azdevops.NotifStorePath()
		if err != nil {
			return fmt.Errorf("resolve notifications state path: %w", err)
		}
		notifStore, err := azdevops.NewTriageStore(notifPath)
		if err != nil {
			return fmt.Errorf("load notifications state: %w", err)
		}
		azureNotifStore = notifStore

		lookbackDays, sourceToggles, minPollInterval := azureNotificationArgs(cfg.Notifications.Azure)
		backends = append(backends, azdevops.NewAdapterWithNotifications(
			client, notifStore, lookbackDays, sourceToggles, minPollInterval))
	}

	// --- GitHub backend (only when at least one repo is configured) ---
	if cfg.HasGitHub() {
		token, err := store.GetGitHubToken()
		if err != nil {
			return fmt.Errorf(
				"GitHub token not found: run 'azdo auth' or set the GITHUB_TOKEN environment variable: %w", err)
		}

		conv := github.LabelConvention{
			TypePrefix:     cfg.GitHub.TypePrefix,
			PriorityPrefix: cfg.GitHub.PriorityPrefix,
		}
		ghMC, err := github.NewMultiClient(cfg.GitHub.Repos, token, conv, nil)
		if err != nil {
			return fmt.Errorf("failed to create GitHub client: %w", err)
		}
		ghNC := github.NewNotificationsClient(token)

		// The done store persists mark-done tombstones locally because
		// GitHub's REST API can write done state but never read it back
		// (github.DoneEntry's doc comment) — without it, every `d`-marked
		// row resurrects on the next launch. Unlike azureNotifStore it needs
		// no shutdown Flush and no variable outliving this block: its writes
		// are synchronous (github.DoneStore's doc comment), so there is
		// never a pending debounced write to lose on exit.
		ghDonePath, err := github.DoneStorePath()
		if err != nil {
			return fmt.Errorf("resolve github notifications state path: %w", err)
		}
		ghDoneStore, err := github.NewDoneStore(ghDonePath)
		if err != nil {
			return fmt.Errorf("load github notifications state: %w", err)
		}
		backends = append(backends, github.NewAdapterWithNotifications(ghMC, ghNC, ghDoneStore))
	}

	// Defense-in-depth: config.Validate() already requires ≥1 backend, but guard
	// here as well so a future caller of runTUI without a prior Validate does not
	// silently produce a zero-backend composite.
	if len(backends) <= 0 {
		return fmt.Errorf("no provider configured: set up Azure DevOps or GitHub (run the setup wizard)")
	}

	composite := provider.NewCompositeProvider(backends...)

	// Load persisted state (last active tab, last-viewed PR/work item).
	// A missing file is treated as a clean slate — not an error.
	statePath, err := state.Path()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}
	stateStore, err := state.NewStore(statePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	// Create and run the TUI application.
	model := app.NewModel(composite, azureMC, cfg, version, commit)
	model.SetStateStore(stateStore)
	model.ApplyState(stateStore.State())
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Forward OS termination signals to Bubble Tea so the program unwinds
	// cleanly (alt-screen restored, state flushed) instead of being killed
	// mid-write. SIGINT is also handled by the in-app 'q'/Ctrl+C binding;
	// SIGTERM and SIGHUP are the ones that matter here.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigCh
		p.Send(tea.QuitMsg{})
	}()

	// Best-effort flush on any exit path — normal quit, signal-driven
	// quit, or a panic propagating up from the Tea program. A SIGKILL or
	// power loss is unrecoverable; the debounced writes during the
	// session bound the loss window.
	defer func() {
		signal.Stop(sigCh)
		if flushErr := stateStore.Flush(); flushErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to persist state: %v\n", flushErr)
		}
		if azureNotifStore != nil {
			if flushErr := azureNotifStore.Flush(); flushErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to persist notifications state: %v\n", flushErr)
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI application error: %w", err)
	}

	return nil
}

// azureNotificationArgs derives azdevops.NewAdapterWithNotifications's three
// notifications.azure-sourced arguments (lookback window, per-source
// toggles, self-throttle interval) from azure. It exists as its own
// function, separate from the constructor call in runTUI above, purely so a
// test can drive a non-default NotificationsAzureConfig through it without
// going through runTUI's keyring/network setup — see
// TestAzureNotificationArgs_NonDefaultValuesReachTheAdapter
// (cmd/azdo-tui/main_test.go). config.NotificationsAzureConfig already
// carries defaults, clamping and validation for LookbackDays and
// MinPollInterval (task 11); this function does no normalisation of its
// own, it only reshapes already-normalised fields into the types
// NewAdapterWithNotifications takes — MinPollInterval is seconds in config
// (matching notifications.azure.min_poll_interval's YAML units) and a
// time.Duration in the adapter.
func azureNotificationArgs(azure config.NotificationsAzureConfig) (lookbackDays int, toggles azdevops.NotificationSourceToggles, minPollInterval time.Duration) {
	return azure.LookbackDays,
		azdevops.NotificationSourceToggles{
			ReviewRequested: azure.Sources.ReviewRequested,
			Mentioned:       azure.Sources.Mentioned,
			Assigned:        azure.Sources.Assigned,
			CIFailed:        azure.Sources.CIFailed,
		},
		time.Duration(azure.MinPollInterval) * time.Second
}

// runSetupWizard launches the interactive setup wizard and saves the config.
func runSetupWizard() (*config.Config, error) {
	if err := requireSystemKeyring(config.NewKeyringStore()); err != nil {
		return nil, err
	}

	model := setupwizard.NewModel()
	p := tea.NewProgram(model)

	m, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("setup wizard error: %w", err)
	}

	finalModel, ok := m.(setupwizard.Model)
	if !ok {
		return nil, fmt.Errorf("unexpected model type from setup wizard")
	}

	if finalModel.Cancelled() {
		return nil, fmt.Errorf("setup cancelled")
	}

	cfg := finalModel.GetConfig()
	if cfg == nil {
		return nil, fmt.Errorf("setup wizard did not produce a configuration")
	}

	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	// Persist the GitHub token to the keyring when the wizard collected one.
	if tok := finalModel.GitHubToken(); tok != "" {
		ks := config.NewKeyringStore()
		if err := ks.SetGitHubToken(tok); err != nil {
			return nil, fmt.Errorf("failed to store GitHub token: %w", err)
		}
	}

	fmt.Println("\nConfiguration saved successfully!")
	return cfg, nil
}

// promptForPAT displays a TUI to prompt the user for their PAT (first-time setup)
func promptForPAT(store *config.KeyringStore) (string, error) {
	return promptForPATWithMode(store, false)
}

// promptForPATWithMode displays a TUI to prompt the user for their PAT.
// If isUpdate is true, shows an "update" message instead of "first-time setup".
func promptForPATWithMode(store *config.KeyringStore, isUpdate bool) (string, error) {
	if err := requireSystemKeyring(store); err != nil {
		return "", err
	}

	var model patinput.Model
	if isUpdate {
		model = patinput.NewModelForUpdate()
	} else {
		model = patinput.NewModel()
	}
	p := tea.NewProgram(model)

	m, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("failed to run PAT input: %w", err)
	}

	// Extract the final model
	finalModel, ok := m.(patinput.Model)
	if !ok {
		return "", fmt.Errorf("unexpected model type")
	}

	pat := finalModel.GetPAT()
	if pat == "" {
		return "", fmt.Errorf("PAT input cancelled or empty")
	}

	// Save the PAT to the system keyring.
	if err := store.SetPAT(pat); err != nil {
		return "", fmt.Errorf("failed to save PAT: %w", err)
	}

	return pat, nil
}
