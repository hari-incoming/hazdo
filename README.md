# Hazdo

Hazdo is a community fork of [AZDO](https://github.com/Elpulgo/azdo), maintained at [github.com/hari-incoming/hazdo](https://github.com/hari-incoming/hazdo). It is a terminal interface for Azure DevOps and GitHub, letting you manage pull requests, work items, and pipelines from your terminal. The executable command remains `azdo` for compatibility.

Point it at Azure DevOps projects, GitHub repositories, or both at once: lists fan out across every configured backend and detail actions route back to the right one. A small glyph (⬢ Azure, ⎇ GitHub) marks each row's origin only when a list mixes backends.

## What Hazdo can do

- Combine Azure DevOps projects and GitHub repositories in one terminal interface.
- Track GitHub notifications and Azure DevOps activity in a unified inbox.
- Browse and manage pull requests and work items, including reviews, comments, votes, and state updates.
- Monitor pipeline runs, inspect timelines and logs, and queue runs with parameter or variable overrides.
- Browse, create, copy, and manage Azure pipeline definitions; press `P` in **All Pipelines** to add a project to the config (restart to load it).
- Use the optional metrics dashboard to review work-item flow and sprint trends.
- Store interactive credentials in the system keyring or credential manager.

![Tests](https://img.shields.io/github/actions/workflow/status/hari-incoming/hazdo/ci.yml?label=tests)
![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)
![License](https://img.shields.io/badge/License-MIT-blue.svg)
![GitHub Release](https://img.shields.io/github/v/release/hari-incoming/hazdo)

## Table of Contents

- [Installation](#installation)
- [Changelog](CHANGELOG.md)
- [Features](#features)
- [Demo Mode](#demo-mode)
- [CLI Usage](#cli-usage)
- [Configuration](#configuration)
- [Keyboard Shortcuts](#keyboard-shortcuts)
- [Technology Stack](#technology-stack)
- [Development](#development)
- [FAQ](#faq)
- [Contributing](#contributing)
- [License](#license)

## Installation

### Clone and Run (Recommended)

**Linux, macOS, or WSL:**
```bash
git clone --depth 1 https://github.com/hari-incoming/hazdo.git && cd hazdo && ./install.sh
```

**Windows (PowerShell):**
```powershell
git clone --depth 1 https://github.com/hari-incoming/hazdo.git hazdo; if ($LASTEXITCODE -eq 0) { & .\hazdo\install.ps1 }
```

On Windows, the installer uses the default user WSL distribution when one is available, reports its Linux distribution and version, and installs the Linux build there. If WSL has no usable distribution, it installs the native Windows build. Pass `-NativeWindows` to `install.ps1` to select the native Windows build explicitly. The same Linux release works across supported WSL distributions; the installer does not need a distro-specific package manager.

### Direct Install (without cloning)

**Linux / macOS:**
```bash
curl -fsSL https://raw.githubusercontent.com/hari-incoming/hazdo/main/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/hari-incoming/hazdo/main/install.ps1 | iex
```

The install scripts will automatically:
- Detect your OS and architecture
- Detect Linux distribution details and whether Linux is running under WSL
- Download the latest release from GitHub
- Install the binary to the appropriate location
- Guide you through first-run setup when you start `azdo`
- Verify the download checksum

The installers require a published GitHub release. If the [Releases page](https://github.com/hari-incoming/hazdo/releases) is empty, use the source-build instructions below until the first release is available.

**Install options:**
```bash
# Install a specific version
curl -fsSL https://raw.githubusercontent.com/hari-incoming/hazdo/main/install.sh | sh -s -- --version v0.1.0

# Install to a custom directory
./install.sh --install-dir ~/bin
```

### Manual Download

Download the latest release for your platform from the [Releases page](https://github.com/hari-incoming/hazdo/releases).

| Platform | Architecture | File |
|----------|-------------|------|
| Linux    | x86_64      | `azdo_*_Linux_x86_64.tar.gz` |
| Linux    | ARM64       | `azdo_*_Linux_arm64.tar.gz` |
| macOS    | x86_64      | `azdo_*_Darwin_x86_64.tar.gz` |
| macOS    | ARM64 (M1+) | `azdo_*_Darwin_arm64.tar.gz` |
| Windows  | x86_64      | `azdo_*_Windows_x86_64.zip` |
| Windows  | ARM64       | `azdo_*_Windows_arm64.zip` |

Extract the archive and move the binary to a directory in your `PATH`.

### From Source

```bash
git clone https://github.com/hari-incoming/hazdo.git
cd hazdo
go build -o azdo ./cmd/azdo-tui
```

### Using Go Install

From a cloned Hazdo checkout, run:

```bash
go install ./cmd/azdo-tui
```

## Features

### Backends
- **Azure DevOps** — projects via WIQL queries and the REST v7.1 API
- **GitHub** — repositories via REST + GraphQL; issues map to work items, labels map to item type / priority / tags, pull-request reviews map to votes, and GitHub Actions runs map to pipelines
- **Mix freely** — configure Azure projects, GitHub repos, or both. Every list view fans out across all configured backends; the per-row origin glyph (⬢ / ⎇) appears only when a list actually mixes backends
- See [Configuration](#configuration) for how to set up each backend

### Multi-Tab Interface
- **Notifications** (Tab 1): A merged "what needs me now" feed — your GitHub inbox plus a synthesized Azure DevOps feed — see [Notifications](#notifications) below
- **Pull Requests** (Tab 2): View and track pull requests, copy PR URLs, and approve and complete Azure DevOps PRs with fast-forward (rebase) or squash
- **Work Items** (Tab 3): Browse and manage work items (GitHub issues appear here too)
- **Active Pipelines**: Monitor and drill into recent pipeline runs (GitHub Actions runs appear here too)
- **All Pipelines**: Browse every Azure pipeline definition across configured projects, create or copy YAML pipelines, rename or move definitions, search and inspect run history, edit YAML parameters and variables, then run them
- Switch between tabs using the number keys or `←`/`→` arrow keys — only enabled tabs are numbered, so the exact digits shift with your config

### Notifications

A default-on "what needs me now" pane merging every configured backend's attention feed
into one list. It's the first tab, shown automatically the moment at least one configured
backend supports it:

- **GitHub** — your actual notifications inbox, read from GitHub's own notifications API.
- **Azure DevOps has no inbox API**, so its share of the feed is *synthesized* from four
  polled sources instead of fetched: pull requests awaiting your review, @mentions in
  work-item discussions, work items recently assigned to you, and your failed pipeline
  runs. Because Azure has no server-side read/done state, read and dismissed markers are
  tracked locally — see [Local Triage State](#local-triage-state-azure-devops) below.

Mix freely: configure Azure, GitHub, or both, and both halves merge into the same feed.

- One merged feed across every backend that supports it, sorted newest-first
- Columns are `● | Repo | Reason | Title | Updated`. Unread rows carry a `●` marker and a
  bold title; read rows are unmarked. The Repo column is always shown, even when every
  visible row is from the same repo/project — this pane is a cross-repo inbox, so which
  repo a row belongs to is primary context and shouldn't disappear when a filter narrows
  the feed
- GitHub rows from repos you haven't configured are shown too (unless
  `only_configured_repos: true`) — `o` opens them in the browser. Azure only ever queries
  the projects you've configured, so this doesn't apply there
- `f` cycles the reason filter (review requested, mentioned, assigned, authored, commented,
  state changed, CI activity, security alert, approval requested, subscribed, other) —
  only reasons present in the current feed are offered, and there's always an "all reasons"
  position
- `u` marks the selected row read — the `●` marker clears. Neither backend supports an
  explicit unmark: GitHub has no mark-unread endpoint, and on Azure the locally tracked
  read flag only clears again once new activity is detected on that subject. With
  `unread_only: false` the row stays in place, unmarked
- `d` marks the selected row done (removes it from the feed)
- `o` opens the selected row in your browser
- An unread-count badge appears in the footer from every tab, after your config filters
  are applied, and hides entirely at zero
- Polling cadence differs per backend: GitHub honors its `X-Poll-Interval` response hint so
  conditional requests never count against your rate limit harder than necessary; Azure
  self-throttles independently via `notifications.azure.min_poll_interval`, since a single
  poll fans out to four or more queries per configured project
- See [Notifications Configuration](#notifications-configuration) below for every filter
  knob, and [Azure DevOps — Personal Access Token](#azure-devops--personal-access-token-pat) /
  [GitHub — Personal Access Token](#github--personal-access-token) for token requirements

### Pull Requests
- List view of pull requests with status indicators
- On Azure DevOps, press `n` in the PR list to create a pull request; choose a project/repository and branches, enter a title, then type or paste a multiline description. Press `Enter` for a new line, `Ctrl+J` or `Alt+Enter` to review the summary (some terminals treat `Ctrl+Enter` as a regular `Enter`), then confirm with `y`/`yes` to submit
- Filter to show only your created PRs (`m` key) or PRs where you're a reviewer (`A` key)
- Detailed view showing PR information and metadata
- Edit an Azure DevOps PR summary/title from its detail view with `s`, or its description with `e`; in either editor, press `Esc` for Vim normal mode, `ggdG` to clear all text, and `i` to resume editing. Press `Enter`/`Ctrl+Enter` to save the summary, or `Ctrl+Enter` to save the description (`Enter` adds a line); press `Esc` again to cancel
- Vote on PRs directly from the detail view. The picker is backend-aware: Azure exposes the full five-level scale (approve, approve with suggestions, wait for author, reject, reset), while GitHub offers just Approve / Request changes to match its review model
- **Code review**: Diff viewer with file-by-file navigation
- Inline commenting, thread replies, and thread resolution
- General (non-file-specific) comments

### Work Items
- List view of work items with status and type information
- On Azure DevOps, press `n` in the work-item list to create an item: choose a project when multiple are configured, enter its type and title, optionally assign it using an email/UPN or Azure DevOps identity, then add an optional multiline description. Press `Enter` to advance through fields (`Enter` adds a line in the description), then `Ctrl+S` to create; leave the assignee blank to create it unassigned. For bugs, the description is saved as repro steps
- Detailed view showing work item details
- View the Discussion (comments) below the description, newest first
- Add comments from the detail view (`c` key, multi-line form)
- Change work item state directly from the detail view (dynamically fetches available states)
- Filter to show only your assigned items
- Filter by tag (`T` key)
- Filter by state (`s` key)

### Pipeline Dashboard
- View recent pipeline runs in a sortable table
- Show a live elapsed duration for running pipeline runs after they start
- Color-coded status indicators (✓ Success, ✗ Failed, ● Running, ○ Queued)
- Filter by status (`S` key)
- Live auto-refresh with configurable polling interval
- In **Run New**, press `x` on a selected queue-time variable to set its value to empty for that run
- In **Run New**, press `d` on an overridden YAML parameter to use its pipeline default for that run
- In **All Pipelines** details, press `A` to save a variable to the pipeline definition for future runs; choose whether it is secret before entering its value. Lowercase `a` still adds a variable for the current run only.
- Connection status indicator in footer
- Hierarchical detail view with stages, jobs, and tasks
- Duration tracking for each step
- Full log viewer with scrollable viewport; press `/` to search log output, `n`/`N` to jump between matches, and `c` to copy the full raw log to the clipboard
- Double-click a word to select it and use your terminal's copy shortcut (Ctrl+Shift+C in Windows Terminal). Mouse clicks and wheel controls are passed through to the terminal, so use keyboard shortcuts to navigate the TUI.

The Azure-only **All Pipelines** tab lists definitions whether or not they have recent runs. It is tab 1 when available, and **Active Pipelines** (recent runs) is tab 2. Notifications is tab 5 when all standard tabs are enabled. Press `/` to search by name, folder path, or project; use `j`/`k` to move and `v` to open run history for the selected pipeline (or `enter` to open its detail and press `v` there). In the All Pipelines list, press `s` to sort by pipeline ID or name, and `S` to reverse the current sort order. History loads 100 runs per page and `o` loads older pages when available. Select a run and press `enter` to inspect its timeline and logs; `esc` backs out one level at a time. In Azure run details, press `p` to review pending protected-resource approval requests in that project; inspect each request's instructions, then press `a` to approve the selected one. This requires the **Pipeline Resources (Use)** PAT scope. In a log viewer, press `c` to copy the complete raw log to the clipboard. YAML runtime parameters appear with their defaults; press `zP` (type `z`, then `P`) for a focused list, and press `zP` again or `esc` to close it. In **Active Pipelines** detail, `zP` opens the parameters captured for that specific run. Press `n` there to start a new run of the same pipeline: a dedicated editor lists all YAML runtime parameters and queue-time variables with their current values. Compatible parameter values from the selected run are copied into the editor; current YAML defaults are used for missing or incompatible values. Use `j`/`k` to select a row and `e` to edit it; `Enter` saves each value to the current run draft, and `r` saves the overrides and queues the new run. If a queue request fails, press `D` to inspect the detailed Azure error or `c` to copy it. These are per-run overrides and do not change the pipeline YAML defaults. `esc` returns to the timeline. In All Pipelines detail, press `N` to rename the pipeline and `M` to move it to another folder (enter `\` for the project root); `e` edits the branch, YAML runtime parameters, or queue-time-overridable variables; `a` adds a queue-time variable; `d` resets an override; and `r` queues the run. Secret variables are never shown, and their input is hidden. Press `n` in the All Pipelines list to create a YAML pipeline: choose a project and repository, select a branch (defaulting to the repository's default branch), choose an existing `.yml`/`.yaml` file from that branch, set a name, and optionally add variables. The wizard detects a top-level YAML pool and preselects a matching available agent queue; you can select another queue, or keep the YAML pool when there is no matching queue (for example, a `vmImage`). If the YAML has no pool configured, choose an agent queue or continue without a default queue. Press `/` to filter project, repository, branch, or YAML-file choices. Mark secret variables when prompted; their values are entered without terminal echo and shown only as `[secret]`. Press `c` in the All Pipelines list to copy a YAML pipeline: choose a different YAML file while retaining its repository, branch, folder, queue, and non-secret variables. Azure DevOps does not reveal stored secret values, so copied pipelines require secret variables to be re-entered. Pipeline creation and queueing are live Azure DevOps actions.

### Metrics Dashboard (opt-in)

A management view for team leads, **disabled by default**. Enable it via `metrics.enabled: true` in `config.yaml` and an additional tab appears.

> **Azure DevOps only.** Metrics relies on WIQL queries and work-item revision history (`/updates`), so it reads from your Azure projects directly and ignores any configured GitHub repos.

Two sub-views, toggled with `v`:

- **Live** — current-state dwell per work item, per-user roll-up (WIP, in-flight, oldest Active / Ready for Test, points closed in the configured interval), and a worst-first "stuck items" pane. Sourced from the live work-item fetch — no local state, on-demand refresh only.
- **Trends** — sprint-on-sprint comparison from a local 90-day snapshot file. Pick any combination of sprint tags with `T` (multi-select; space toggles, enter confirms) and see per-user **points closed**, **average WIP**, **stuck count**, and **cycle time** side-by-side. Values are colored: green for closed points, yellow when overloaded, red for stuck items.

The snapshot file lives at `~/.config/azdo-tui/metrics.jsonl`. One row per work item per day is appended on first metrics-tab launch each day, then pruned to a 90-day window. No database — append-only JSONL.

**One-shot backfill (optional).** A fresh install starts with an empty snapshot file, so the Trends view shows "Insufficient snapshot history" for the first ~2 sprints. To seed the file from your team's actual recent history, set:

```yaml
metrics:
  run_one_shot_backfill: true
```

On the next launch the tab walks every in-flight or recently-closed work item across all configured projects, reads each item's revision history via `/updates`, and synthesizes daily snapshot rows back 90 days. The footer reports progress and the result. A marker file (`~/.config/azdo-tui/.metrics-backfill-done`) prevents re-running — delete it if you want to re-seed. Flip the flag back to `false` once it's done so the footer hint stops appearing.

### User Experience
- **Setup wizard** on first run guides you through configuration
- Help modal with all keyboard shortcuts (press `?`)
- Secure token storage through the system keyring/credential manager; interactive setup stops if it is unavailable
- Context-aware keybinding hints
- Graceful error handling with automatic retry
- Eight built-in themes with true color support
- **Theme switcher** modal (press `t`) to change themes on the fly
- **Multi-project and multi-repo support** across Azure DevOps and GitHub, with display name customization
- **State persistence** — remembers the last active tab and the last opened PR / work item detail across sessions, so you can pick up where you left off

## Demo Mode

Want to try Hazdo without an Azure DevOps or GitHub account? Run the demo — no configuration, no token, no setup required:

```bash
azdo demo
```

This launches the full TUI with realistic mock data (two fictional projects, pull requests with diffs, work items, pipeline runs with logs). All features work — you can navigate, view details, switch themes, and explore the UI. Perfect for evaluating the tool or taking screenshots.

## CLI Usage

```bash
# Start the TUI
azdo

# Try it out with mock data (no setup needed)
azdo demo

# Set or update credentials (prompts for the backend: Azure PAT or GitHub token)
azdo auth

# Show version
azdo --version

# Show help
azdo --help

# List and search every Azure pipeline definition, including idle pipelines
azdo pipeline list --search deploy

# Queue a pipeline by name or definition ID (defaults to the current Git branch)
azdo pipeline run "CI Build" --branch feature/my-change

# Interactive PR creation: select repository/branches, enter a title/message, confirm
azdo pr create --interactive

# Create an Azure DevOps pull request non-interactively from the current Git branch
azdo pr create --repo MyRepository --title "Add my change" --target main
```

## Configuration

### 1. Create Configuration File

When running azdo for the first time, a **wizard setup** will help you setup this.
Otherwise follow these instructions.

Create a configuration file at the following location:
- **Linux/macOS**: `~/.config/azdo-tui/config.yaml`
- **Windows**: `C:\Users\<username>\.config\azdo-tui\config.yaml`

Configure at least one backend — Azure DevOps, GitHub, or both.

```yaml
# ── Azure DevOps (optional; required only if you use Azure) ──────────────
# Organization name. Required when any Azure projects are listed.
organization: your-org-name

# Azure DevOps project name(s).
# Simple format:
projects:
  - your-project-name

# With display names (friendly name shown in UI):
#   projects:
#     - name: ugly-api-project-name
#       display_name: My Project
#     - name: ugly-api-project-name-2
#       display_name: My Project 2

# ── GitHub (optional; required only if you use GitHub) ───────────────────
# One or more "owner/repo" slugs. Listing at least one enables the GitHub
# backend. Issues become work items, PRs become pull requests, and Actions
# runs become pipelines.
# github:
#   repos:
#     - your-org/your-repo
#     - your-org/another-repo
#   # Label prefixes used to derive a work item's type and priority from
#   # GitHub issue labels (case-insensitive). Defaults shown; omit to use them.
#   type_prefix: "type:"        # e.g. "type:bug" → Bug, "type:feature" → Feature
#   priority_prefix: "priority:" # e.g. "priority:p1" or "priority:1" → priority 1
#   # Labels that match neither prefix are shown as tags.

# Polling interval in seconds (optional, default: 60)
polling_interval: 60

# Theme (optional, default: dark)
# Available themes: dark, gruvbox, nord, dracula, catppuccin, github, retro, monokai
theme: dark

# Disable specific panes (optional, comma-separated)
# Valid values: pullrequests, pipelines, workitems, notifications
# At least one pane must remain enabled.
# disabled_panes: pipelines,workitems

# Notifications tab (on by default whenever Azure or GitHub is configured).
# There is no notifications.enabled key — add "notifications" to
# disabled_panes above to turn the tab off. Shared keys apply to the merged
# feed regardless of backend; github/azure keys are provider-specific. See
# "Notifications Configuration" below for the full reference.
# notifications:
#   exclude_repos: []
#   include_repos: []
#   exclude_reasons: []
#   unread_only: false
#   max_items: 0
#   poll_interval: 0
#   github:
#     participating_only: false
#     only_configured_repos: false
#     since_days: 0
#   azure:
#     lookback_days: 14
#     min_poll_interval: 300
#     sources:
#       review_requested: true
#       mentioned: true
#       assigned: true
#       ci_failed: true

# Tab labels (optional). Override the name shown for any tab, in both the tab
# bar and the help dialog. Keys are lowercase snake_case; unset tabs keep their
# defaults.
# terms:
#   pull_requests: PRs
#   work_items: Tasks
#   pipelines: Builds
#   metrics: Dashboard

# Metrics dashboard (opt-in, management feature). Hidden unless enabled.
# See "Metrics Configuration" below for the full reference.
# metrics:
#   enabled: false
#   interval_days: 14            # window for the Live "closed pts" column
#   active_stale_days: 3         # dwell in Active above this flags the item
#   rft_stale_days: 2            # dwell in Ready for Test above this flags the item
#   wip_limit: 4                 # in-flight strictly above this marks a user overloaded
#   run_one_shot_backfill: false # one-time /updates seed (see Features → Metrics)
#   states:                      # your board's actual state names (case-insensitive)
#     active: Active
#     ready_for_test: Ready for Test
#     closed: Closed
#   state_labels:                # optional column-header overrides (auto-derived if omitted)
#     active: active
#     ready_for_test: rft
#     closed: closed
```

**Configuration Options:**
- `organization`: Your Azure DevOps organization name. Required when `projects` is set.
- `projects`: List of Azure DevOps project names. Each entry can be a plain string or an object with `name` and `display_name` fields. The `display_name` is shown in the TUI while the `name` is used for API calls.
- `github.repos`: List of GitHub `owner/repo` slugs. Listing at least one enables the GitHub backend.
- `github.type_prefix`: Label prefix used to derive a work item's type from GitHub issue labels (optional, default: `type:`). A label like `type:bug` maps to Bug; recognised values are bug, task, story / user story, feature, epic, issue. An unrecognised value (e.g. `type:chore`) is kept as a tag instead.
- `github.priority_prefix`: Label prefix used to derive priority from GitHub issue labels (optional, default: `priority:`). Accepts `p1`–`p4` or bare `1`–`4`; anything else is kept as a tag.
- **At least one backend is required** — set Azure (`organization` + `projects`), GitHub (`github.repos`), or both.
- `polling_interval`: How often to refresh data in seconds (optional, default: 60)
- `theme`: Color theme for the UI (optional, default: dark)
- `disabled_panes`: Comma-separated list of panes to hide (optional). Valid values: `pullrequests`, `pipelines`, `workitems`, `notifications`. When a pane is disabled, its tab, keyboard shortcuts, and all related UI are removed, and remaining tabs are renumbered. At least one pane must remain enabled — a config with only `notifications` enabled is valid as long as at least one notification-capable backend (Azure DevOps or GitHub) is configured. Adding or removing `notifications` here only takes effect on the **next restart**; the tab list is computed once at startup.
- `terms`: Map of tab label overrides (optional). Keys are lowercase snake_case (`pull_requests`, `work_items`, `pipelines`, `metrics`); the value replaces the tab's name in both the tab bar and the help dialog. Unset tabs keep their default labels.
- `metrics`: Opt-in management dashboard. See [Metrics Configuration](#metrics-configuration) below for the full reference, and [Features → Metrics Dashboard](#metrics-dashboard-opt-in) for what it does.
- `notifications`: Default-on merged attention feed (no `enabled` key — see `disabled_panes` above to turn it off). Shared keys sit at the top level; GitHub-only and Azure-only keys are nested under `notifications.github` and `notifications.azure`. See [Notifications Configuration](#notifications-configuration) below for the full reference, and [Features → Notifications](#notifications) for what it does.

**Available Themes:**
- `dark` - Dark theme with blue and cyan accents
- `gruvbox` - Retro groove color scheme
- `nord` - Arctic, north-bluish color palette
- `dracula` - Default dark theme with purple and pink accents
- `catppuccin` - Soothing pastel theme (Mocha variant)
- `github` - GitHub Dark theme
- `retro` - Matrix-inspired green phosphor on black
- `monokai` - Classic Monokai color scheme

### Metrics Configuration

The metrics dashboard is **opt-in and hidden entirely** unless `metrics.enabled: true`. All keys live under the top-level `metrics:` block and are optional — the defaults below apply when a key is omitted. The validation rules only apply when `enabled` is `true`.

| Key | Type | Default | Description |
|---|---|---|---|
| `metrics.enabled` | bool | `false` | Master switch. The whole tab is hidden when `false`. |
| `metrics.interval_days` | int | `14` | Look-back window (days) for points-closed / velocity. Must be `> 0`. |
| `metrics.active_stale_days` | int | `3` | Dwell in Active longer than this flags the item as stuck. Must be `>= 0`. |
| `metrics.rft_stale_days` | int | `2` | Dwell in Ready-for-Test longer than this flags the item as stuck. Must be `>= 0`. |
| `metrics.wip_limit` | int | `4` | In-flight items *strictly above* this marks a user overloaded (⚠). Must be `> 0`. |
| `metrics.run_one_shot_backfill` | bool | `false` | One-time 90-day `/updates` seed of history on next launch. A marker file prevents it re-running. |

#### State names — `metrics.states`

These map your board's **actual workflow-state strings** onto the three buckets the metrics engine tracks. Matching is **case-insensitive and whitespace-trimmed**, but each takes a **single name** (no comma-separated aliases). If your board doesn't literally use "Active" / "Ready for Test" / "Closed", set these or the metrics tab will bucket nothing.

| Key | Default |
|---|---|
| `metrics.states.active` | `Active` |
| `metrics.states.ready_for_test` | `Ready for Test` |
| `metrics.states.closed` | `Closed` |

Each name must be non-empty, **distinct** from the other two, and contain no single quote (`'`) — single quotes are rejected for WIQL-injection safety.

#### Column labels — `metrics.state_labels` (optional)

Display-only overrides for the metrics table column headers. When omitted, labels are **auto-derived** from the configured state name: multi-word names become lowercased initials (`Ready for Test` → `rft`, `In Progress` → `ip`), single-word names are lowercased as-is (`Done` → `done`).

| Key | Falls back to |
|---|---|
| `metrics.state_labels.active` | derived from `states.active` |
| `metrics.state_labels.ready_for_test` | derived from `states.ready_for_test` |
| `metrics.state_labels.closed` | derived from `states.closed` |

**Example — a board using "Doing" / "QA" / "Done":**

```yaml
metrics:
  enabled: true
  interval_days: 14
  wip_limit: 4
  states:
    active: Doing
    ready_for_test: QA
    closed: Done
  state_labels:
    ready_for_test: QA   # override the auto-derived lowercase "qa" to keep the caps
```

### Notifications Configuration

The Notifications tab is **on by default** the moment at least one notification-capable
backend is configured — Azure (`organization` + `projects`), GitHub (`github.repos`), or
both — there is deliberately no `notifications.enabled` key. To turn it off, add
`notifications` to `disabled_panes` instead (see [Configuration Options](#configuration)
above); either way it takes effect on the **next restart**, not immediately.

The `notifications:` block is nested: a handful of keys apply to the **merged feed
regardless of backend** and stay at the top level, while every key that only makes sense
for one backend is nested under `notifications.github` or `notifications.azure`. Getting
this distinction backwards is the easiest way to misconfigure this pane — a key you expect
to affect both backends but that only lives under `github` (or vice versa) will silently
do nothing for the other one. Note that an unrecognised key is **ignored silently** — it is
not a startup error and not a warning — so a key left at the old top level, or nested under
the wrong backend, looks live in your config file while doing nothing at all. If a knob
seems to have stopped working, check its nesting first.

#### Shared keys (top level — apply to both backends)

| Key | Type | Default | Description |
|---|---|---|---|
| `notifications.exclude_repos` | []string | `[]` | Glob list of scopes to hide from the feed. Empty excludes nothing. |
| `notifications.include_repos` | []string | `[]` | Glob list narrowing the feed to matching scopes. Empty means no narrowing. Narrowing is **per backend** — a pattern that addresses only GitHub leaves the Azure share of the feed untouched, and vice versa; see the callout below. **Ignored entirely** when `notifications.github.only_configured_repos` is `true` — see that key below. |
| `notifications.exclude_reasons` | []string | `[]` | Reasons to drop from the feed — see "Reason values" below. |
| `notifications.unread_only` | bool | `false` | Show only unread rows. The fetch itself always requests the whole feed regardless of this setting; filtering happens client-side. |
| `notifications.max_items` | int | `0` (no cap) | Caps the number of notifications in the **merged, sorted** feed across every backend, applied after the newest-first sort — not per-backend, so `max_items: 50` means at most 50 rows total even with both backends live. |
| `notifications.poll_interval` | int | `0` | Overrides the global `polling_interval` for the notifications poller only, in seconds. `0` falls back to the global `polling_interval`. Either way, a backend's own cadence hint (GitHub's `X-Poll-Interval` response header) can **raise** the interval above what you configured but never lower it — the poller runs at `max(configured, hint)`, so `polling_interval: 300` with a 60-second hint still polls every 300 seconds. This is the single poller's cadence — it is unrelated to `notifications.azure.min_poll_interval` below, which the Azure adapter applies to itself independently of this poller's tick rate. |

**`exclude_repos` / `include_repos` address the two backends differently.** Both are glob
lists matched against a row's scope, but what that scope *is* differs: on GitHub it's the
`owner/repo` slug; on Azure it's the plain project name. An Azure project can be written
either way — bare (`"your-project"`) or qualified with your `organization`
(`"your-org/your-project"`, and so `"your-org/*"` for every project in it) — so one list
can address both backends in the same shape.

**`include_repos` narrows each backend separately.** A backend is filtered only if at
least one pattern actually addresses it; a backend no pattern addressed keeps everything.
That's what makes `include_repos` mean "a subset of what I configured" rather than "the
only backend I remembered to name":

```yaml
notifications:
  include_repos: [my-gh-owner/*]     # narrows GitHub; every Azure project still shows
  # include_repos: [my-org/proj-a]   # narrows Azure to proj-a; all of GitHub still shows
  # include_repos: [my-gh-owner/*, my-org/proj-a]   # narrows both
  # include_repos: []                # (default) everything, both backends
```

A pattern counts as addressing Azure when it matches one of your configured `projects`,
bare or org-qualified. Every other pattern is treated as a GitHub pattern. That means a
**typo in an Azure project name fails open**: `my-org/porj-a` matches no project, is read
as a GitHub pattern, and leaves Azure unnarrowed — you get too many rows rather than an
empty feed. Because that failure is otherwise silent, a pattern that starts with your
organization name but matches no project gets a startup warning.

#### GitHub-only keys (`notifications.github`)

| Key | Type | Default | Description |
|---|---|---|---|
| `notifications.github.only_configured_repos` | bool | `false` | Narrow the **GitHub share** of the feed to repos listed in `github.repos`; rows from any other backend pass through untouched. When `true`, this **overrides** `include_repos` entirely — `include_repos` is then ignored for *every* row, not just the GitHub ones, with a startup warning. The two selection knobs never intersect. |
| `notifications.github.participating_only` | bool | `false` | Narrow the **server-side** fetch to GitHub's "participating" bundle (roughly everything except `subscribed`). Composes with `exclude_reasons` rather than replacing it — see "Precedence" below. |
| `notifications.github.since_days` | int | `0` (no bound) | Only fetch GitHub notifications updated within the last N days. This is the knob to reach for on a very large inbox — it's the one filter that actually reduces what's fetched from GitHub, rather than merely trimming the client-side result. |

#### Azure-only keys (`notifications.azure`)

| Key | Type | Default | Description |
|---|---|---|---|
| `notifications.azure.lookback_days` | int | `14` | Bounds the "recently assigned work items" and "failed pipeline runs" sources to activity within the last N days. The other two sources are bounded by their own nature rather than by a date: "PRs awaiting my review" is the set still open and still awaiting you, and "@mentions" is whatever Azure's own recent-mentions query returns. Clamped to a maximum of 30 days. Unlike `github.since_days`, **`0` is not "unbounded" here** — an explicit `lookback_days: 0` falls back to the default instead, because an unbounded assigned-work-item query means "every work item ever assigned to me". |
| `notifications.azure.min_poll_interval` | int (seconds) | `300` | The shortest interval between two real Azure notification queries; the adapter self-throttles to this and returns its previous result on calls made sooner. Also falls back to the default on an explicit `0`, for the same reason as `lookback_days`. Clamped to a maximum of 86400 seconds. |
| `notifications.azure.sources.review_requested` | bool | `true` | Include "PRs awaiting my review" (emits reason `review_requested`). |
| `notifications.azure.sources.mentioned` | bool | `true` | Include "@mentions in work-item discussions" (emits reason `mentioned`). |
| `notifications.azure.sources.assigned` | bool | `true` | Include "recently assigned work items" (emits reason `assigned`). |
| `notifications.azure.sources.ci_failed` | bool | `true` | Include "my failed pipeline runs" — completed runs you requested whose result is `failed`. A canceled or partially-succeeded run is deliberately not a failure. **This key names the source, not the reason** — see the callout below. |

**`sources.ci_failed` names a source, not a reason — the two spellings are not the same
vocabulary.** The toggle is called `ci_failed` because that's what the source *queries*
("my failed pipeline runs"). What it *emits* into the feed is the `ci_activity` reason —
the same reason value `exclude_reasons` accepts. So `sources.ci_failed: false` turns the
source off entirely (no rows, no query), while `exclude_reasons: [ci_activity]` leaves the
query running but drops its rows client-side afterwards. Don't write `ci_failed` in
`exclude_reasons` expecting it to match anything — it isn't a reason name and will just
produce an unrecognised-value warning.

**Turning all four sources off is legal** — it yields an empty Azure share of the feed
without an error, and it does not hide the tab. If you want the tab gone, use
`disabled_panes`.

**Each Azure source fetches at most 50 items per project per poll.** This is a fixed
internal bound, not something `max_items` controls — `max_items: 0` means "don't cap the
merged feed", not "fetch everything". `@mentions` is capped twice: 50 candidate work items
per project, then 50 again across all projects merged, so in a three-project org mentions
truncate at roughly 17 per project.

Three of the four sources ask the server for the newest items first — work items by last
changed, pipeline runs by finish time — so what survives their cap is the most recent. "PRs
awaiting my review" sends no ordering at all, so which 50 you get is whatever the server
returns first. Either way, in a busy org with more than 50 recently assigned work items or
more than 50 mention candidates, the rest are simply not fetched, and nothing in the pane
tells you so. Narrowing `lookback_days` helps for assigned work items and failed runs — a
shorter window means the 50 you do get are the ones you care about. For mentions and review
requests there is no such knob: `lookback_days` does not reach either source.

**Repo glob syntax.** `exclude_repos` / `include_repos` patterns are `path.Match` globs,
matched case-insensitively (both the pattern and the scope are lower-cased first). `*`
does not cross `/`, so `*` matches a bare Azure project name but no `owner/repo`, and
`*/*` matches every GitHub repo and every org-qualified Azure project. You rarely need
either: an empty `include_repos` already means "everything". A pattern that
fails to compile is dropped at load with a startup warning rather than rejected outright,
and an `include_repos` list that loses every pattern this way falls back to showing the
whole feed rather than emptying it.

**Reason values.** `exclude_reasons` accepts the following eleven values, lowercase
snake_case, and they are **case-sensitive** — config keys are lowercased by viper on load,
but list *values* are passed through unchanged, so `Subscribed` does not match `subscribed`;
an unrecognised value (including a differently-cased one) produces a startup warning naming
the bad value and is then dropped rather than applied or treated as a hard config error:

`review_requested`, `mentioned`, `assigned`, `authored`, `commented`, `state_changed`,
`ci_activity`, `security_alert`, `approval_requested`, `subscribed`, `other`

`unknown` is **reserved and not accepted** — it exists internally as the enum's zero value
but nothing either backend sends is ever mapped to it, so listing it in `exclude_reasons`
can only ever produce the same unrecognised-value warning as a typo.

**Precedence.** The shared and GitHub-only filter knobs are not a pipeline and their
evaluation order is not observable — each is an independent predicate over the row set,
except for one genuine override: `notifications.github.only_configured_repos` and
`include_repos` are both *selection* knobs, and when both are set,
`only_configured_repos` wins outright: `include_repos` is ignored for *every* row, with a
warning, and rows from any backend other than GitHub pass through unnarrowed. So
`include_repos: ["proj-a"]` together with `only_configured_repos: true` does **not** narrow
your Azure rows to `proj-a` — it shows Azure rows from every project.
`exclude_repos`, `exclude_reasons`, and `unread_only` are a plain, order-independent AND —
each one only ever removes rows, never adds them back, so it doesn't matter which is
"applied first".

```yaml
notifications:
  exclude_repos:
    - "some-org/noisy-repo"
  exclude_reasons:
    - subscribed
    - ci_activity
  unread_only: false
  poll_interval: 120
  github:
    since_days: 30
  azure:
    lookback_days: 7
    sources:
      ci_failed: false
```

### Custom Themes

You can create your own custom themes by placing JSON theme files in the themes directory:
- **Linux/macOS**: `~/.config/azdo-tui/themes/`
- **Windows**: `C:\Users\<username>\.config\azdo-tui\themes\`

**Creating a Custom Theme:**

1. Create the themes directory if it doesn't exist:
   ```bash
   mkdir -p ~/.config/azdo-tui/themes
   ```

2. Create a JSON theme file (e.g., `mytheme.json`):
   ```json
   {
     "name": "mytheme",
     "primary": "#0088ff",
     "secondary": "#00aaff",
     "accent": "#ff8800",
     "success": "#00ff88",
     "warning": "#ffaa00",
     "error": "#ff4444",
     "info": "#00ccff",
     "background": "#1a1b26",
     "background_alt": "#24283b",
     "background_select": "#343b58",
     "foreground": "#c0caf5",
     "foreground_muted": "#787c99",
     "foreground_bold": "#ffffff",
     "select_foreground": "#ffffff",
     "select_background": "#0088ff",
     "border": "#3b4261",
     "link": "#7aa2f7",
     "spinner": "#bb9af7",
     "tab_active_foreground": "#ffffff",
     "tab_active_background": "#0088ff",
     "tab_inactive_foreground": "#787c99"
   }
   ```

3. Set the theme in your `config.yaml`:
   ```yaml
   theme: mytheme
   ```

4. Restart the application to use your custom theme.

See `example-theme.json` in the repository for a complete template with all available color properties. Colors can be specified as:
- Hex values: `#ff0000` or `#f00`
- ANSI 256 colors: `"1"`, `"33"`, `"196"`

### State File

The application persists a small amount of navigation state between runs (last active tab, last opened PR / work item detail) so you land back where you left off. The file is written to:

- **Linux/macOS**: `$XDG_STATE_HOME/azdo-tui/state.yaml` if set, otherwise `~/.local/state/azdo-tui/state.yaml`
- **Windows**: `%USERPROFILE%\.local\state\azdo-tui\state.yaml`

The file is created lazily — no state file is required to run the app. Writes are debounced and flushed on clean exit (including SIGINT / SIGTERM / SIGHUP). Delete the file to reset the saved view.

### Local Triage State (Azure DevOps)

Azure DevOps has no server-side notifications inbox, so there is no read/done state to read
back from the API the way there is for GitHub. Read and dismissed ("done") markers for the
Azure share of the Notifications feed are instead tracked in a small local file, alongside
each row's identity key and the activity timestamp used to detect when a dismissed row
should resurface. The file is written to:

- **Linux/macOS**: `$XDG_STATE_HOME/azdo-tui/notifications.yaml` if set, otherwise `~/.local/state/azdo-tui/notifications.yaml`
- **Windows**: `%USERPROFILE%\.local\state\azdo-tui\notifications.yaml`

It lives next to `state.yaml` (same directory resolution) but as its own separate file — a
missing file loads as empty, not an error, and writes are debounced and flushed on clean
exit the same way `state.yaml`'s are. Deleting `state.yaml` does not touch it, and deleting
`notifications.yaml` does not touch your navigation state. Delete `notifications.yaml` to
clear your Azure triage history — every Azure row then returns unread, which on a busy org
means a large feed on the next poll.

**This state is local to the machine it runs on and is not synced across machines.**
Marking an Azure notification read or done on one machine has no effect on any other — only
GitHub's read/done state lives on GitHub's own servers and follows you everywhere you sign
in with the same token.

### 2. Authentication

Run `azdo auth` and pick the backend you want to set credentials for (Azure DevOps or GitHub) — the first run prompts automatically. Configure whichever backend(s) you use. Interactive token setup requires your system's credential manager:
- **Windows**: Windows Credential Manager
- **macOS**: Keychain
- **Linux**: Secret Service (gnome-keyring, KWallet, etc.)

The `azdo auth` prompt masks the token as you type; it does not echo it into
the terminal or put it in shell history. Prefer this interactive flow over
typing a token in a shell command. Avoid putting tokens in shell startup files:
environment variables can be visible to processes running as your user.

If the credential manager is unavailable, setup stops before prompting for a
token. Install or enable the platform credential manager and retry. On Linux,
that means an unlocked Secret Service provider such as GNOME Keyring or KWallet;
Windows Credential Manager and macOS Keychain are provided by the OS.
`AZDO_PAT` and `GITHUB_TOKEN` remain available for non-interactive use when
injected from a trusted secret manager. To update credentials at any time, run
`azdo auth`.

#### Enable GNOME Keyring on WSL 2 (Ubuntu)

WSL does not always start a Linux desktop/keyring session by default. On a
current WSL 2 installation, enable systemd for the Ubuntu distribution so
GNOME Keyring can provide the Linux Secret Service:

1. In PowerShell, check/update WSL:
   ```powershell
   wsl --version
   wsl --update
   ```
2. In Ubuntu, edit `/etc/wsl.conf` and add the following section. If the file
   already has a `[boot]` section, add `systemd=true` to that section instead
   of creating a duplicate:
   ```ini
   [boot]
   systemd=true
   ```
3. From PowerShell, restart WSL:
   ```powershell
   wsl --shutdown
   ```
   Then reopen Ubuntu.
4. Install GNOME Keyring:
   ```bash
   sudo apt update
   sudo apt install gnome-keyring
   ```
5. In Ubuntu, confirm systemd is PID 1 and inspect the keyring user units:
   ```bash
   ps -p 1 -o comm=
   systemctl --user list-unit-files 'gnome-keyring*'
   ```
   The first command should print `systemd`. Enable the unit your Ubuntu
   release provides (run only the matching command):
   ```bash
   # Ubuntu 22.04
   systemctl --user enable --now gnome-keyring.service

   # Ubuntu 24.04
   systemctl --user enable --now gnome-keyring-daemon.socket
   ```
   If `systemctl --user` reports that it cannot connect to the bus, the WSL
   user session is not available to that shell; the keyring will not be usable
   there yet. See Microsoft's
   [WSL systemd guide](https://learn.microsoft.com/en-us/windows/wsl/systemd)
   and the Ubuntu [`gnome-keyring-daemon` manual](https://manpages.ubuntu.com/manpages/noble/man1/gnome-keyring-daemon.1.html).

Finally, run `azdo auth` and confirm the success message says **system keyring**.
If the Secret Service is unavailable or locked, setup stops and asks you to
enable/unlock it before retrying. If prompted to create/unlock a keyring, use a
strong password. Headless WSL sessions may not be able to show a keyring-unlock
prompt, and WSL does not automatically unlock the keyring like a desktop login
might; installing the daemon alone does not guarantee that the keyring is
available or unlocked.

#### Azure DevOps — Personal Access Token (PAT)

**Required PAT Scopes:**
| Scope | Access | Used For |
|-------|--------|----------|
| **Build** | Read & Execute | List and queue pipeline runs, build timelines, and logs |
| **Pipeline Resources** | Use | Approve pending requests to use protected pipeline resources |
| **Code** | Read & Write | List PRs, view threads/iterations/diffs, vote on PRs, add comments, and update thread status |
| **Work Items** | Read & Write | Query and view work items, read/add comments, fetch available states, and change work item state |

**No additional scope is needed for the Azure share of the Notifications tab.** All four
Azure notification sources reuse existing scopes — including the work-item @mentions
source, which calls the same work-item comments endpoint the Work Items pane already uses
under the **Work Items** scope above.

To create a PAT:
1. Go to Azure DevOps → User Settings → Personal Access Tokens
2. Click "New Token"
3. Select the required scopes
4. Copy the generated token

#### GitHub — Personal Access Token

A classic or fine-grained token works for most operations.

**Classic PAT** — https://github.com/settings/tokens:
| Scope | Used For |
|-------|----------|
| `repo` | Private repos: issues, pull requests, reviews, and Actions |
| `public_repo` | Public repos only (use instead of `repo`) |
| `notifications` | The Notifications tab: list, mark read, mark done (**classic only** — see note below) |

**Fine-grained token** — https://github.com/settings/personal-access-tokens — grant these
repository permissions:
| Permission | Access |
|------------|--------|
| Metadata | Read |
| Contents | Read |
| Issues | Read & write |
| Pull requests | Read & write |
| Actions | Read |

> **Note:** resolving PR comment threads requires a classic `repo` PAT — fine-grained tokens are commonly rejected for that operation.

> **The GitHub share of the Notifications tab requires a classic PAT.** This is a GitHub
> API limitation, not a limitation of this app or of the Azure DevOps side of the feed:
> GitHub's notifications REST API
> [only supports classic tokens](https://docs.github.com/en/rest/activity/notifications) —
> there is no fine-grained permission that enables it, so don't go looking for one in the
> fine-grained token form. A fine-grained token is fine for everything else in the GitHub
> backend; it just can't reach the GitHub inbox. An Azure-configured backend needs no extra
> scope at all (see [Azure DevOps — Personal Access Token](#azure-devops--personal-access-token-pat)
> above), so an **Azure-only** setup gets a fully working Notifications tab. If you configure
> **both** backends and your GitHub token can't reach the inbox, the tab currently shows the
> GitHub scope error instead of your Azure rows — the failing backend takes the whole pane
> with it rather than degrading to the half that works. This is symmetric: an expired Azure
> PAT blanks a healthy GitHub inbox the same way, so whichever backend the error names is
> the one to fix. Either grant the GitHub token the `notifications` scope, or remove
> `github.repos` from your config — note that this drops GitHub from the other tabs too, not
> just from notifications. If you'd rather not switch your GitHub
> token, see [Disabling the Notifications tab](#notifications-configuration) — add
> `notifications` to `disabled_panes` and the tab goes away entirely.

**Upgrading an existing token.** If you already have a GitHub token configured from before
the Notifications tab existed, it won't have the `notifications` scope and the tab will
show a "token scope required: notifications" error instead of your inbox. Edit the token at
https://github.com/settings/tokens, tick `notifications`, and run `azdo auth` to update the
stored token. If your existing token is fine-grained, you need a new classic one — see the
note above.

**Diagnosing a 403.** Classic tokens make GitHub send back which scopes are required and
which the token has, so a missing-scope 403 names both. Fine-grained tokens never send
those headers, so the exact same 403 shows up bare — and that absence is itself the
diagnosis: a bare 403 here almost always means the token is fine-grained and needs
replacing with a classic one. Note a 403 isn't always a scope problem either: a
rate-limited 403 is reported as a plain failure with no scope banner at all, since adding a
scope to an already-correct token wouldn't fix a rate limit.

To create a token:
1. Go to GitHub → Settings → Developer settings → Personal access tokens
2. Generate a classic or fine-grained token with the scopes above (classic if you want the
   Notifications tab)
3. Copy the generated token

## Keyboard Shortcuts

### Global
| Key | Action |
|-----|--------|
| `1`-`6` | Switch tabs (All Pipelines, Active Pipelines, PRs, Work Items, Notifications, Metrics when Azure is configured) — only enabled tabs are numbered |
| `←/→` or `h/l` | Previous / next tab |
| `r` | Refresh data |
| `↑/↓` or `j/k` | Navigate up/down |
| `gg` / `G` | Jump to first / last row |
| `pgup/pgdn` | Page up/down |
| `enter` | View details / expand |
| `f` or `/` | Search / filter (searchable lists) — `f` cycles the reason filter on Notifications instead, see below |
| `m` | Toggle my items (PRs / work items) |
| `A` | Toggle as reviewer (PRs) |
| `T` | Filter by tag (work items) |
| `s` | Filter by state (work items) |
| `S` | Filter by status (pipelines) |
| `esc` | Go back / dismiss search |
| `?` | Toggle help modal |
| `t` | Select theme |
| `q` or `Ctrl+C` | Quit |

### Notifications Tab
| Key | Action |
|-----|--------|
| `f` | Cycle the reason filter (only reasons present in the current feed, plus an "all reasons" position) |
| `u` | Mark selected row read (one-way on GitHub, which has no mark-unread endpoint; on Azure the local read flag only clears again once new activity is detected on that subject) |
| `d` | Mark selected row done (removes it from the feed) |
| `o` | Open selected row in browser |
| `x`, `x` | Only shown when the pane is in an error state (e.g. missing token scope) — press twice to disable the tab via `disabled_panes`, takes effect on next restart |

### PR Detail View
| Key | Action |
|-----|--------|
| `v` | Vote on pull request |
| `o` | Open pull request in browser |
| `c` | Copy pull request URL to clipboard |
| `m` | Approve and complete an active Azure DevOps PR (fast-forward/rebase or squash; confirmation required) |
| `s` | Edit Azure DevOps PR summary/title |
| `e` | Edit Azure DevOps PR description |
| `enter` | View diff for selected file |

### PR List View
| Key | Action |
|-----|--------|
| `n` | Start interactive Azure DevOps PR creation (when Azure DevOps is configured) |

### PR Diff / Code Review View
| Key | Action |
|-----|--------|
| `c` | Create comment (on selected line or general) |
| `p` | Reply to nearest thread |
| `x` | Resolve nearest thread |
| `n` | Jump to next comment |
| `N` | Jump to previous comment |
| `r` | Refresh changed files |

### Work Item Detail View
| Key | Action |
|-----|--------|
| `w` | Change work item state |
| `c` | Add a comment (opens form; `Ctrl+S` to send, `Esc` to cancel) |
| `o` | Open work item in browser |

### Pipeline Detail View
| Key | Action |
|-----|--------|
| `j/k` or `gg/G` | Navigate stages / jobs / tasks |
| `enter` | Expand / collapse a node, or view logs for a leaf task |
| `zM` / `zR` | Collapse / expand all pipeline details |
| `o` | Open the pipeline run in browser |
| `f` or `/` | Search / filter timeline records; use `j`/`k` to move through matches |
| `r` | Refresh |

### All Pipelines View
| Key | Action |
|-----|--------|
| `/` | Search pipeline names, paths, and projects |
| `s` | Cycle sorting between pipeline ID and name (ascending) |
| `S` | Reverse the current sort order |
| `enter` | Open definition and load variables |
| `c` | Copy the selected YAML pipeline with its variables |
| `n` | Create a YAML pipeline |
| `v` (`H` also works) | Browse run history for the selected definition (`o` loads older pages) |
| `e` | Edit branch or selected variable |
| `N` | Rename the selected pipeline |
| `M` | Move the selected pipeline to another folder (`\` for project root) |
| `A` | Add and save a variable to the pipeline definition (secret or non-secret) |
| `a` | Add a queue-time variable for the current run only |
| `d` | Remove the selected variable override |
| `r` | Queue pipeline with current branch and variable overrides |

### Log Viewer
| Key | Action |
|-----|--------|
| `/` | Search log output |
| `n` / `N` | Jump to the next / previous matching line |
| `r` | Refresh logs |
| `g` | Jump to top |
| `G` | Jump to bottom |

## Technology Stack

- **Go 1.23+**
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - Terminal UI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components (table, viewport)
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Styling and layout
- [Viper](https://github.com/spf13/viper) - Configuration management
- [go-keyring](https://github.com/zalando/go-keyring) - Secure credential storage

## Development

### Running Tests

```bash
go test ./...
```

### Running with Coverage

```bash
go test -cover ./...
```

### Building

```bash
go build -o azdo ./cmd/azdo-tui
```

### Releases

See [RELEASES.md](RELEASES.md) for release process and GoReleaser usage.

## FAQ

See [FAQ.md](FAQ.md) for common questions and troubleshooting.

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
