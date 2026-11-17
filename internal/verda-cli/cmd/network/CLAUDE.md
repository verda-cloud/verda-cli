# Network Command Knowledge

> Go house style lives in the root `CLAUDE.md` § "Go House Style". This file carries private-network idioms only.

## Quick Reference

- Parent: `verda network` (aliases `networks`, `net`, `private-network`, `vpc`), tagged `(beta)` via `cmdutil.TagAnnotation`, registered under "Resource Commands:" in `cmd/cmd.go`.
- Verbs: `list` (`ls`), `create`, `delete` (`rm`); `subnet` (`subnets`) with `list` (`ls`), `create` and `delete` (`rm`).
- SDK: `client.PrivateNetworks` (experimental in the SDK).
- Files: one per verb; `cidr.go` (CIDR checks, free /24), `views.go` (JSON/YAML shapes), `wizard.go` (create flows); tests use the in-memory API in `fake_test.go`.

## Domain-Specific Logic

- **A network is global; a subnet is per location.** Network names are unique in a project.
- **Default network.** A VM that does not name a network joins the default network. `create --default` makes the new network the default.
- **Mode.** `custom` (default): the user creates the subnets. `auto`: only for the project's default network, a deploy that names no subnet makes the API create one in its location, from the location's `private_network_auto_subnet` range. A deploy that names a non-default auto network gets a 400 if the location has no subnet.
- **Names** are trimmed on every path (flag and wizard) and limited to 64 characters, counted in runes, not bytes.
- **IDs only.** Arguments and `--network` take IDs, never names. `delete` reads the network with `GetByID`; `subnet delete` needs `--network` and reads the subnet with `GetSubnetByID`. An unknown ID is the API 404, which `ClassifyError` reports as `NOT_FOUND`. `subnet list` and `subnet create` match `--network` against the network list and return `NOT_FOUND` themselves. Subnet names are unique in a network: `subnet create` checks a new name against them.
- **Delete with instances: the API refuses (409) and changes nothing.** The CLI does not check first; `ClassifyError` reports the 409 as `API_ERROR` (exit 4). It never deletes instances.
- **Network delete removes its subnets.** The confirmation lists them (one extra `GetSubnets` call, skipped with `--yes`).
- **`is_default` on network create is only sent when true** (`CreatePrivateNetworkRequest.IsDefault` is `omitempty`); the test checks the exact body.
- **`is_default` on subnet create is a `*bool`.** The CLI sets it only when the user sets `--default` or answers the wizard step. `--default=false` sends `false`, and the API keeps it, also for the first subnet. Nil lets the API make the first subnet of a location its default. For that reason, the wizard skips the step for the first subnet in a location.
- **Subnet CIDR.** `freeSubnetCIDR` suggests the first free /24 in 10.0.0.0/16. `checkCIDR` refuses an overlap with any subnet of the network, in all locations, before the POST. With `--network`, the command reads only that network's subnets.
- **Subnet locations.** Only locations with `IsPrivateNetworksEnabled`. Another location gives `VALIDATION_ERROR` that lists the allowed codes.

## Gotchas & Edge Cases

- **AgentErrors print as JSON outside agent mode too** (`main.go`): `create` without `--name`, `subnet create` without `--network` / `--location`, `delete` without a target, or `subnet delete` without a target or `--network`, on a pipe gives the `MISSING_REQUIRED_FLAGS` envelope with exit 2.
- **Agent mode without `--yes`** fails with `CONFIRMATION_REQUIRED` before any API call, for both deletes.
- **Pickers** run only on a terminal, outside agent mode, with table output. The `subnet delete` picker lists the subnets of `--network` when given, else of all networks. Cancel (the last row, Esc, Ctrl+C) prints `Canceled.` to stderr and exits cleanly, like a declined confirmation.

## Relationships

- `cmdutil` — `WithSpinner`, `RunWithSpinner`, `DebugJSON`, `WriteStructured`, `TimeColumn`, `TextColumn`, `NewMissingFlagsError`, `NewValidationError`, `NewConfirmationRequiredError`.
- `pkg/tui/wizard` — the create flows.
