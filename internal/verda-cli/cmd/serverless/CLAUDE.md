# Serverless Command Knowledge

> Go house style lives in the root `CLAUDE.md` § "Go House Style". This file carries serverless-specific idioms only — see below for the two-SDK-service split, scaling-preset math, the "spot = container-only" invariant, and the fixed-storage contract that do not apply elsewhere.

## Quick Reference

- Parent: `verda serverless` (no aliases)
- Subcommands:
  - `verda serverless container` → `/container-deployments` (continuous endpoints, supports spot)
  - `verda serverless batchjob` → `/job-deployments` (one-shot jobs, deadline-based, **no spot**)
- Verbs (both trees): `create`, `list` (alias `ls`), `describe` (aliases `get`, `show`), `delete` (aliases `rm`, `del`), `pause`, `resume`, `purge-queue`. Container also has `restart`.
- Files:
  - `serverless.go` — Parent command. Registered in `cmd/cmd.go` under the "Serverless Commands" group. **No feature gate, no `Hidden: true`** — this is a GA feature, unlike s3/registry.
  - `container.go`, `batchjob.go` — Subcommand parents.
  - `container_create.go` — `containerCreateOptions`, flags, `request()`, validate(), wizard entry point.
  - `container_list.go` — `GetDeployments` + tabwriter + structured output.
  - `container_describe.go` — `GetDeploymentByName` + `GetDeploymentStatus` (best-effort) + `selectContainerDeployment` picker.
  - `container_delete.go` — `DeleteDeployment(timeoutMs)` + destructive confirm.
  - `container_actions.go` — Data-driven action factory `newContainerActionCmd` (pause/resume/restart/purge-queue).
  - `batchjob_create.go` — `batchjobCreateOptions` (simpler: no spot, deadline required).
  - `batchjob_list.go`, `batchjob_describe.go`, `batchjob_delete.go`, `batchjob_actions.go` — Same shape as container, trimmed.
  - `shared.go` — `validateDeploymentName` (RFC-1123 subset), `rejectLatestTag`, `parseEnvFlag`, `parseSecretMountFlag`, `confirmDestructive`, `statusColor`, `mountType*` + `envType*` constants.
  - `wizard.go` — 22 step definitions + `buildContainerCreateFlow`. Step-per-field with defaults, so `make test` passes without a mock TUI because every wizard step is just a closure.
  - `wizard_cache.go` — `apiCache` with lazy loaders for compute resources, registry creds, secrets, file secrets. Shared across wizard passes so back-navigation doesn't re-hit the API.
  - `wizard_subflows.go` — `promptEnvVar`, `promptSecretMount` for the two loop-add steps.
  - `wizard_summary.go` — `renderContainerSummary` prints the review card before the final confirm. **Not** a wizard step — rendered from `runContainerCreate` after the flow returns.
  - `*_test.go` alongside each file.

## Domain-Specific Logic

### Two SDK services, two subcommands

The web UI's "Deployment type: Continuous | Job" radio maps to two separate SDK services and two separate HTTP paths:

- `ContainerDeploymentsService` at `/container-deployments` — full `ContainerScalingOptions` (min/max replicas, ScalingTriggers with QueueLoad + CPU/GPU util, scale-up/down policies, request TTL, concurrent requests). Supports `IsSpot: true`.
- `ServerlessJobsService` at `/job-deployments` — thin `JobScalingOptions` (`MaxReplicaCount`, `QueueMessageTTLSeconds`, `DeadlineSeconds`). No min replicas, no triggers, no scale-up/down policies, no spot option.

Consequence: **the CLI never re-asks** deployment type inside the wizard. Pick the subcommand, get the right API shape.

### Scaling preset mapping (CRITICAL)

`queue-preset` → `ScalingTriggers.QueueLoad.Threshold`:

| Preset | Threshold |
|--------|-----------|
| `instant` | 1 |
| `balanced` (default) | 3 |
| `cost-saver` | 6 |
| `custom` | value of `--queue-load` (1..1000) |

Setting `--queue-load N` alone (without `--queue-preset custom`) is also accepted and behaves as custom. The preset name is NOT persisted server-side — on describe, the CLI reverses the mapping for display (threshold 1/3/6 → the named preset, else "custom: N"). See `resolveQueueLoad` in `container_create.go`.

Aliases accepted for the "cost-saver" preset: `cost_saver`, `costsaver` — underscore and camel-case forms show up in copy-pasted configs, so we normalize.

### :latest tag rejection

Both `container create` and `batchjob create` call `verda.IsLatestTag(image)` via `rejectLatestTag` before the API call. The SDK also rejects in `ValidateCreate*DeploymentRequest`, but we fail fast with a friendly error before spinner. Tests: `TestRejectLatestTag`, `TestContainerRequest_RejectsLatest`, `TestBatchjobRequest_RejectsLatest`.

### Deployment name format

`[a-z0-9]([-a-z0-9]*[a-z0-9])?`, max 63 chars (RFC-1123 subset, URL-safe). Becomes part of `https://containers.datacrunch.io/<name>`. Immutable after create — the server refuses updates. `validateDeploymentName` enforces; tests cover edge cases (uppercase, underscore, leading/trailing hyphen, too long, empty).

### Storage defaults are fixed today

General storage (`/data`, 500 GiB) and SHM (`/dev/shm`, 64 MiB) are labeled "fixed for now" in the web UI. The wizard does NOT prompt for them — `renderContainerSummary` shows them as "(fixed)" in the review card, and the create request always includes both mounts with the default sizes. Flags `--general-storage-size` and `--shm-size` exist for the future when the API unlocks them; today they default to the fixed values.

Mount types in `ContainerVolumeMount.Type`:

- `"secret"` — from `--secret-mount NAME:PATH`; `SecretName` set
- `"shared"` — general `/data` storage; `SizeInMB` set
- `"shm"` — `/dev/shm`; `SizeInMB` set

See `buildVolumeMounts` in `container_create.go`.

### Batchjob cannot use spot

`batchjobCreateOptions` has no `Spot` field and no `--spot` flag. `CreateJobDeploymentRequest` has no `IsSpot`. The user asked for this invariant up front — if the web UI ever adds spot to jobs, revisit both structs and the wizard at the same time.

### Deadline is required for batchjob

`JobScalingOptions.DeadlineSeconds` must be `> 0` — enforced in `batchjobCreateOptions.request()`, in the SDK via `ValidateCreateJobDeploymentRequest`, and listed in `missingBatchjobCreateFlags`. The batchjob wizard (when implemented — see Gotchas) must include a deadline prompt as a required field.

### Action-command factory pattern

`newContainerActionCmd` / `newBatchjobActionCmd` build a `*cobra.Command` from `(verb, short, spinner, successMsg, destructive, fn)`. This avoids five nearly-identical files per subcommand. If you need to add an action (e.g. a future `scaling get`), add a new call site with the right SDK method. If you need per-action flags beyond `--yes`, you'll have to step out of the factory — acceptable if one action grows special, not if two do.

### Destructive confirms

`restart` and `purge-queue` are marked destructive (they break in-flight requests); `pause` and `resume` are not. In agent mode, destructive actions require `--yes` and return `CONFIRMATION_REQUIRED` otherwise. Non-agent TTY uses `confirmDestructive` from `shared.go` (red warning + "cannot be undone" line + `prompter.Confirm`).

### Status color and card rendering

`statusColor(status)` in `shared.go` heuristically picks green (running/active/healthy), red (error/failed), dim (paused/stopped/offline), yellow (transitional) by substring match. There's no SDK enum for deployment status — the server returns free-form strings today. Keep the matcher lenient.

Describe cards (`renderContainerDeploymentCard`, `renderJobDeploymentCard`) print one `Label  value` line per section, using color-6 bold for labels. Env var VALUES are intentionally not printed — only names — since values may contain secrets.

## Gotchas & Edge Cases

- **Wizard omits healthcheck sub-prompts when Off.** Steps `healthcheck-port` and `healthcheck-path` have `ShouldSkip: c["healthcheck"] == "off"`. Don't call them unconditionally; the engine wires the skip gate via `DependsOn`.
- **`registryPublicValue = "__public__"` sentinel.** The registry-creds step's loader prepends a "Public (no credentials)" choice with this sentinel as its Value. The Setter maps the sentinel back to `opts.RegistryCreds = ""`. If you rename the sentinel, grep both sides — the Setter reads the string literal.
- **`compute-size` is a separate step from `compute`.** VM's wizard combines resource + count in a single step via in-Loader prompting; serverless keeps them separate so users can go back and change the size without re-picking the resource. Lower engineering cost, same UX.
- **Util triggers off by default, but wizard asks anyway.** The CPU/GPU util steps accept empty ("off"), "off", or `1..100`. Setter maps empty/"off" to 0 (trigger disabled). Users should be able to Enter-through both without setting them.
- **Custom queue-load is a separate step.** `queue-load-custom` has `ShouldSkip: c["queue-preset"] != "custom"`. If the user goes back and changes preset to a named one, the engine's reset logic clears the custom value via `Resetter`.
- **No `+ Create new` for registry creds in the wizard.** v1 intentionally omits the inline create-new sub-flow for registry credentials — users pick existing or Public. Adding new creds requires `verda registry configure` out-of-band, or a future top-level `verda serverless registry-creds` command. The design doc notes this as future work.
- **Confirm is NOT a wizard step.** `runContainerCreate` prints the summary + runs `prompter.Confirm` after `engine.Run` returns. Keeps the review card at full terminal width and lets us pipe through `--yes` cleanly. If you move it into the wizard, you lose layout control.
- **Agent mode + create = flag-only.** In `--agent`, if any of `--name/--image/--compute` is missing we return `MISSING_REQUIRED_FLAGS` immediately. The wizard is never launched under `--agent`, even without credentials — that would be an interactive prompt, which is blocked.
- **Batch-job wizard is NOT implemented yet.** `batchjob create` with missing flags errors out with "interactive wizard is coming" pointing at the design doc. The flag-driven path fully works. Follow-up: factor the shared wizard steps out of `wizard.go` and build a 13-step job flow.
- **Scaling preset + legacy rows on describe.** When a deployment was created via the web UI with a custom queue-load (say 10), our CLI shows "custom: 10" rather than a named preset. Don't try to round-trip it back to a named preset — exact threshold wins.
- **`ContainerDeployment.Status` is NOT in the `GetDeployments` list response.** The list endpoint returns `ContainerDeployment` without status. We call `GetDeploymentStatus(name)` per-row in `describe`, but NOT in `list` (would N+1). If the web UI grows a bulk status endpoint, wire it in.
- **Env-var name validation:** `^[A-Z_][A-Z0-9_]*$`. Lowercase or leading-digit names are rejected client-side in both `parseEnvFlag` and `promptEnvVar`. This is stricter than POSIX (which allows lowercase) but matches Verda's conventions.
- **Secret mount path must be absolute.** `parseSecretMountFlag` and `promptSecretMount` both check `strings.HasPrefix(path, "/")`. No trailing-slash normalization is applied.
- **`DeleteDeployment` timeout semantics:** `--timeout-ms` flag maps directly to the SDK's `timeoutMs` parameter. `-1` (default) uses the API default of 60s; `0` returns immediately; `>0` waits up to that many ms (capped at 300000 server-side).

## Relationships

- `cmdutil` (`internal/verda-cli/cmd/util`) — `Factory`, `IOStreams`, `WithSpinner`, `RunWithSpinner`, `DebugJSON`, `WriteStructured`, `NewMissingFlagsError`, `NewConfirmationRequiredError`, `UsageErrorf`, `LongDesc`, `Examples`, `DefaultSubCommandRun`.
- `verdagostack/pkg/tui/wizard` — `Flow`, `Step`, `Choice`, `Store`, `Engine`, `NewEngine`, `StaticChoices`, `WithOutput`, `WithExitConfirmation`, prompt-type enums.
- `verdagostack/pkg/tui` — `Prompter`, `Status`, `WithConfirmDefault`.
- SDK (`verdacloud-sdk-go/pkg/verda`):
  - `ContainerDeploymentsService` — `GetDeployments`, `CreateDeployment`, `GetDeploymentByName`, `DeleteDeployment`, `GetDeploymentStatus`, `PauseDeployment`, `ResumeDeployment`, `RestartDeployment`, `PurgeDeploymentQueue`, `GetServerlessComputeResources`, `GetRegistryCredentials`, `GetSecrets`, `GetFileSecrets`, `ValidateCreateDeploymentRequest`.
  - `ServerlessJobsService` — `GetJobDeployments`, `CreateJobDeployment`, `GetJobDeploymentByName`, `DeleteJobDeployment`, `GetJobDeploymentStatus`, `PauseJobDeployment`, `ResumeJobDeployment`, `PurgeJobDeploymentQueue`, `ValidateCreateJobDeploymentRequest`.
  - Types: `ContainerDeployment`, `JobDeployment`, `JobDeploymentShortInfo`, `CreateDeploymentRequest`, `CreateJobDeploymentRequest`, `ContainerScalingOptions`, `JobScalingOptions`, `ScalingTriggers`, `QueueLoadTrigger`, `UtilizationTrigger`, `ScalingPolicy`, `ContainerCompute`, `ContainerRegistrySettings`, `RegistryCredentialsRef`, `ContainerEnvVar`, `ContainerVolumeMount`, `ContainerHealthcheck`, `ContainerEntrypointOverrides`, `ComputeResource`, `Secret`, `FileSecret`, `RegistryCredentials`.
- `charm.land/lipgloss/v2` — status color styles + describe-card labels.
- `github.com/spf13/cobra` — command plumbing.
