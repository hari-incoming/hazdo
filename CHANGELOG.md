# Changelog

Notable user-facing changes to Hazdo are recorded here.

## Unreleased

### Pull requests

- **Copy a PR URL:** press `c` in PR detail to copy its URL to the clipboard without opening a browser.
- **Approve and complete a PR:** on Azure DevOps, press `m` in PR detail, choose fast-forward (rebase) or squash, and confirm. The app approves the PR and then completes it with that merge strategy.
- **Review a multiline PR description:** `Enter` inserts a newline; use `Ctrl+J` or `Alt+Enter` to review before submitting. This avoids terminals where `Ctrl+Enter` is indistinguishable from `Enter`.

### Pipelines

- **Inspect and share queue errors:** when a run request fails, press `D` to open scrollable error details or `c` to copy them. Details include Azure's full HTTP 400 message and metadata plus the endpoint and a request body with parameter/variable values redacted.
- **Copy pipeline logs:** press `c` in the log viewer to copy the complete raw log to the clipboard.
- **Clear a run variable:** in the **Run New** parameter list, press `x` on a selected overridable variable to send an empty value for that run.
- **Sort All Pipelines:** press `s` to cycle between ascending pipeline ID and name sorts, and `S` to reverse the selected order. Sorting is retained when the list refreshes.
- **Edit parameters for a new run:** `n` opens a dedicated list of all YAML runtime parameters and queue-time variables. Edit rows with `e`, save each draft value with `Enter`, then press `r` to save the overrides and queue the run.
- **Reset a copied run parameter:** in **Run New**, press `d` on an overridden parameter to use its YAML default for the new run.
- **Queue complex YAML parameters:** object and list parameters are serialized as YAML text for Azure DevOps' Pipelines Runs API; scalar parameter types remain native JSON values.
- **Live run duration:** the pipeline run list updates the elapsed duration for active runs once per second, without additional Azure DevOps requests.
- **Create a YAML pipeline:** press `n` in **All Pipelines** to choose a project, repository, branch, and `.yml`/`.yaml` file, then set its name and add variables. The wizard detects a top-level pool in the selected YAML and preselects a matching available agent queue; you can choose another queue or keep the YAML pool. If no pool is defined, choose an agent queue or continue without a default queue. The default branch is preselected; `/` filters project, repository, branch, and YAML-file choices.
- **Copy a pipeline:** press `c` in **All Pipelines** to copy a YAML pipeline, choose a different YAML file, and retain its repository, branch, folder, agent queue, and non-secret variables. Azure DevOps does not return stored secret values, so the wizard asks you to re-enter them.
- **Rename a pipeline:** open its detail and press `N`, enter the new name, and save.
- **Move a pipeline between folders:** open its detail and press `M`, then enter the destination folder. Use `\` for the project root.
- The **All Pipelines** footer now lists its create and copy shortcuts, alongside the relevant navigation and refresh actions.
