# Copilot instructions

## Build, test, and checks

Use Go 1.24.2 or a compatible Go 1.24 toolchain.

```bash
go build ./...                         # CI build check
go build -o azdo ./cmd/azdo-tui        # Build the CLI/TUI executable
go test ./...                          # Full test suite (also run in CI)
go test -cover ./...                   # Coverage
sh install_test.sh                     # Installer detection and URL logic (also run in CI)
go test ./internal/ui/pipelines        # Test one package
go test ./internal/ui/pipelines -run '^TestLogViewerModel_SearchAndNavigateMatches$'
go vet ./...                           # Static checks recommended by CONTRIBUTING.md
go fmt ./...                           # Format Go packages
gofmt -l path/to/changed.go            # Must print nothing before committing Go changes
```

CI runs `go build ./...`, `sh install_test.sh`, and `go test ./...`. There is
no separate configured lint command; `go vet ./...` is the repository's
documented static check.

## Architecture

- `cmd/azdo-tui` loads configuration and credentials, constructs configured
  backends, then starts the Bubble Tea app in `internal/app`. The root model
  owns tabs, global shortcuts, overlays, pollers, and persisted navigation.
- UI models use Bubble Tea's model/update/view flow. `tea.Cmd` performs
  asynchronous work and returns messages for models to handle; rendering stays
  in `View()`. The shared generic `internal/ui/components/listview` supplies
  list/detail, filtering, and table behavior to the PR, work-item, and pipeline
  views.
- Views use backend-neutral types and `provider.Provider`. Azure DevOps and
  GitHub each have a single-scope `Client`, a multi-scope client, and an
  `Adapter` that maps API types into `internal/provider` types. The
  `CompositeProvider` fans list calls out across configured adapters, merges
  and sorts results, and routes detail/mutation calls by scope. A failed scope
  should not discard successful results; partial failures use `PartialError`.
- `provider.Identity` identifies an entity by backend `Kind`, API `Scope`, and
  native `ID`. Scope is the routing key: Azure project name or GitHub
  `owner/repo`. Metrics and pipeline-definition management are Azure-specific
  and intentionally use the concrete Azure client alongside the neutral
  provider.
- Metrics calculation and snapshot logic live in `internal/metrics`; the
  Bubble Tea metrics view and Azure API orchestration live in
  `internal/ui/metrics`.
- `Architecture.md` describes the full data flow and design decisions. Tests
  are colocated with packages; provider adapters also have conformance tests.

## Key conventions

- Use `https://github.com/hari-incoming/hazdo` for project documentation, source links,
  install instructions, and release references. Preserve attribution to the
  original AZDO project when changing public-facing documentation.
- Keep views backend-neutral. Map wire fields and backend status strings to
  neutral types/enums at the adapter boundary; use `Identity.Kind` for
  per-row origin, not the provider-wide `Kind()`.
- Every per-scope provider operation, including URL helpers, takes `scope`
  first so the composite can route it. Preserve the full neutral entity shape
  used by downstream views and diff rendering when changing mappings.
- Treat optional backend features as capabilities rather than adding stub
  methods to the core provider contract. `CompositeProvider` can satisfy a
  capability interface unconditionally; use its capability check (for
  example, `HasNotifications()`) instead of type-asserting the composite to
  determine whether a wrapped backend supports the feature.
- For partial multi-scope results, return usable data with a `PartialError`;
  callers should detect it with `errors.As` and keep the successful rows.
- If row builders conditionally add cells (provider glyphs, project columns,
  etc.), derive `ToColumns` from the same predicate and item slice. Exercise
  dynamic table layouts through `View()` after a `WindowSizeMsg`, not only by
  testing row/column helper functions.
- Preserve view-specific glyph and style semantics when using shared display
  helpers; the same state can intentionally render differently in different
  panes.
- Viper map keys are lowercase; use lowercase snake_case for lookups.
- Reject invalid positive identifiers with `<= 0`, not only `== 0`.
- Config save tests must create a config file under `t.TempDir()` and load it
  with `config.LoadFrom(...)` before calling `Save()`. A zero-value
  `config.Config` can otherwise write to the developer's real config path.
- Follow the approved **Active** rules in `.spec/conventions.md`; items under
  **Proposed** are not approved conventions.
