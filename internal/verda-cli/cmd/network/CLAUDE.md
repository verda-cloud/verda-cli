# Network Command Knowledge

> Go house style lives in the root `CLAUDE.md` § "Go House Style". This file carries private-network idioms only.

## Quick Reference

- Parent: `verda network` (aliases `networks`, `net`, `private-network`, `vpc`), tagged `(beta)` via `cmdutil.TagAnnotation`, registered under "Resource Commands:" in `cmd/cmd.go`.
- Subtrees: `network subnet` (alias `subnets`) and `network route` (alias `routes`).
- Verbs: `list` (`ls`), `describe` (`get`, `show`), `create`, `update`, `delete` (`rm`). Routes have `list`, `create`, `delete` only.
- SDK: `client.PrivateNetworks` (experimental in the SDK; workload lists from v1.9.0), `client.Locations`, `client.Instances` (route gateways only).
- Files:
  - `network.go` — parent, shared constants, `interactiveTTY`, `resolveNetworkArg` / `selectNetwork` picker, `yesNo`, `defaultColumn`.
  - `list.go`, `describe.go` (`networkDescribeView`, `writeSubnetTable`), `create.go`, `update.go` (also the shared update flow: `updateOptions`, `runChangeFlow`, `confirmUpdate`, `newEngine`), `delete.go` (also `confirmDestructive`, `instancesInUseError`, `withConflictHint`, `writeDeleted`).
  - `subnet.go` — subnet parent, `resolveSubnetArg`, `loadSubnetRows`, `selectSubnet`.
  - `subnet_list.go`, `subnet_describe.go`, `subnet_create.go` (`subnetCreateData`), `subnet_update.go`, `subnet_delete.go`.
  - `route.go` — route parent, `routeTarget` (`--subnet`/`--network`), `loadRoutes`, `gatewayGone`.
  - `route_list.go` (`routeListView`), `route_create.go`, `route_delete.go`.
  - `wizard.go` — `network create` and `subnet create` wizard flows.
  - `fake_test.go` — stateful in-memory API (`fakeAPI`) plus `runNetwork`, `withTerminal`, factories for table/agent/json modes.
- Shared helpers live in `cmd/util/network.go` (resolve, defaults, CIDR math, `NewResourceInUseError`) and `cmd/util/views.go` (`NetworkView`, `SubnetView`, `RouteView`) so `mk8s` can use them without importing this package.

## Domain-Specific Logic

- **A network is global; a subnet is per location.** Network names are unique in a project, subnet names unique in a network, subnet IDs unique everywhere. So a subnet ID needs no `--network` (`cmdutil.ResolveSubnet` searches all networks); a subnet name that repeats across networks returns a `VALIDATION_ERROR` asking for `--network`.
- **Defaults.** The default network is the one a VM joins when it names none; the first subnet in a location becomes that location's default. `--default` only sets: `--default=false` is rejected, because sending `false` has no defined effect on the current default.
- **Updates send only the chosen fields.** Request types use pointers; tests check the exact PATCH body.
- **Delete with instances: the API refuses (409) and changes nothing.** The CLI checks first (`GetWorkloads`, or `GetSubnetWorkloads` for a subnet) and returns `RESOURCE_IN_USE` with `details.blockers`. It never deletes instances. The workload lists do not include cluster nodes, so a delete that cluster nodes block gets the API's 409 instead.
- **409 on delete.** Deleting an instance is asynchronous, so a delete right after it can still get 409 `conflict`. `withConflictHint` keeps the classified error (`API_ERROR`, `api_code`) and appends "wait a minute and try again". The CLI does not retry on its own.
- **Network delete removes its subnets and routes** (SDK comment and Terraform docs; the live integration test checks it). The warning lists them.
- **Subnet CIDR.** `cmdutil.ValidateSubnetCIDR` mirrors the API: canonical IPv4, `/29` or larger, inside an RFC 1918 range. Overlap is checked against every subnet of the network, in any location. Without `--cidr`, `FreeSubnetCIDR` picks the first free `/24` in `10.0.0.0/16`, and the choice is printed. `is_default` is only sent when asked for: `CreateSubnetRequest.IsDefault` is a `*bool`, and the API keeps an explicit `false`, so sending it would stop the first subnet of a location from becoming its default.
- **Routes.** One route per destination per subnet; a create for an existing destination moves the route, so the CLI confirms a move (`--yes` in agent mode) but not a new route. The gateway must be on the network, in the subnet's location, with a public IP (`gatewayProblem`). After a create, the NAT note goes to stderr in table mode only: the gateway must do IP forwarding and NAT itself.
- **A route whose gateway is gone** stays listed. `gatewayGone` treats a nil instance, or one missing from the live (non-`discontinued`) instance list, as gone: `(deleted)` in the table, `instance_deleted: true` in JSON.

## Gotchas & Edge Cases

- **Locations.** Only locations with `is_private_networks_enabled` take subnets (live: FIN-03 and us-southwest-1; FIN-01/02 are off). The error lists the enabled ones.
- **Capacity.** In FIN-03 every `CPU.*` type answered 503 "No free private-network capable instance type"; `/instance-availability` does not account for private-network capacity. Private-network VMs use `CPU-TURIN.*`.
- **A deleted instance** still answers GET by ID, with status `discontinued` and its network fields null. The route commands drop it from the live instance list (`gatewayGone`).
- **`newEngine` is a package var** so tests can inject `wizard.WithTestResults`; tests that swap it (or `isInteractiveTerminal`) must not call `t.Parallel()`.
- **AgentErrors print as JSON outside agent mode too** (`main.go`): a missing target on a pipe gives the `MISSING_REQUIRED_FLAGS` envelope with exit 2.
- **Agent mode without `--yes`** fails before any API call for every delete. `route create` can only tell it is a move after reading the routes, so its `CONFIRMATION_REQUIRED` (`move route`) comes after the reads.

## Relationships

- `cmdutil` — `ResolveNetwork`, `ResolveSubnet`, `FindNetwork`, `FindSubnet`, `ValidateSubnetCIDR`, `FreeSubnetCIDR`, `SubnetPrefixes`, `DefaultNetwork`, `DefaultSubnet`, `NewResourceInUseError`, the three views, `WithSpinner`, `RunWithSpinner`, `DebugJSON`, `WriteStructured`.
- `pkg/tui/wizard` — create flows and the update change flow.
- `tests/contract/mockapi/networks.go` — mock endpoints for the contract suites.
- `tests/integration/network_test.go` — read-only tests plus the `VERDA_NETWORK_INTEGRATION=1` lifecycle test, run through `runIsolated` (`isolated_test.go`).
