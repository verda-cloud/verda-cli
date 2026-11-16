# verda network -- Manage private networks, subnets and routes (beta)

A private network (VPC) is global; its subnets live in one location each. Instances on the same network reach each other over private IPs. A route sends a subnet's internet traffic through one of your instances (a NAT gateway).

Arguments accept an ID (`pn_…`, `sn_…`) or a name. A subnet name needs `--network` only when several networks use it.

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda network list` | List networks (NAME, ID, MODE, DEFAULT, CREATED) | _(none)_ |
| `verda network describe [<network>]` | Network, its subnets with their route, instance count | _(none)_ |
| `verda network create` | Create a network | `--name`, `--mode custom\|auto`, `--default` |
| `verda network update [<network>]` | Rename and/or make default | `--name`, `--default` |
| `verda network delete [<network>]` | Delete a network with its subnets and routes | `--yes` |
| `verda network subnet list` | Subnets of all networks, or one | `--network`, `-l/--location` |
| `verda network subnet describe [<subnet>]` | Subnet, its route and its instances | `--network` |
| `verda network subnet create` | Create a subnet | `--network`, `-l`, `--name`, `--cidr`, `--default` |
| `verda network subnet update [<subnet>]` | Rename and/or make default (the CIDR cannot change) | `--network`, `--name`, `--default` |
| `verda network subnet delete [<subnet>]` | Delete a subnet and its routes | `--network`, `--yes` |
| `verda network route list` | Routes of a subnet | `--subnet`, `--network` |
| `verda network route create` | Route a subnet's traffic through an instance | `--subnet`, `--network`, `--instance`, `--destination`, `--yes` |
| `verda network route delete [<route-id>]` | Delete a route | `--subnet`, `--network`, `--yes` |

## Usage Examples

### Networks
```bash
# Interactive: name, mode, default
verda network create

# Flags only
verda network create --name prod
verda network create --name staging --mode auto --default

verda network list
verda network describe prod -o json
verda network update prod --name production
verda network update pn_d90ky5tk --default
verda network delete staging
verda --agent network delete pn_d90ky5tk --yes
```

### Subnets
```bash
# Interactive: network, location, name, CIDR (suggested), default
verda network subnet create

# Flags only: the name defaults to <location>-<n>, the CIDR to the first free /24 in 10.0.0.0/16
verda network subnet create --network prod -l FIN-03
verda network subnet create --network prod -l FIN-03 --name web --cidr 10.0.8.0/24 --default

verda network subnet list --network prod -l FIN-03
verda network subnet describe sn_82gp8jck
verda network subnet update web --network prod --default
verda network subnet delete sn_82gp8jck
```

### Routes
```bash
# The gateway must be on the network, in the subnet's location, with a public IP
verda network route create --subnet sn_82gp8jck --instance nat-1
verda network route list --subnet sn_82gp8jck
verda network route delete rt_82gp8jck --subnet sn_82gp8jck
```

After `route create`, enable IP forwarding and NAT (MASQUERADE) on the gateway instance; otherwise the subnet's traffic is dropped.

## Behavior Notes

- **Defaults:** `--default` only sets. To change the default, make another network (or subnet) the default; `--default=false` is rejected.
- **Delete:** a network or subnet with instances is refused, and the instances are listed (hostname, ID, location, private IP). Delete them first with `verda vm action --action delete`. If the API still answers 409 right after, wait a minute and try again.
- **Moving a route:** a subnet has one route per destination. Creating a route for a destination that already has one moves it, after a confirmation.
- **Deleted gateway:** a route whose instance is gone stays and shows `(deleted)` (`instance_deleted: true` in JSON).
- **Interactive mode:** omitting the target on a terminal opens a picker. `update` with no change flags opens a small flow (pick the changes, enter the name, confirm). Off a terminal, in agent mode or with `-o json|yaml`, missing input returns `MISSING_REQUIRED_FLAGS`.

## Agent Mode

- Deletes need `--yes`, as does moving a route; otherwise `CONFIRMATION_REQUIRED` (exit 2).
- A delete blocked by instances returns `RESOURCE_IN_USE` (exit 1) with `details.blockers`.
- API errors carry `details.api_code` (for example `conflict`) when the API sends one.

## Architecture

- Shared helpers in `cmd/util/network.go` (resolve, defaults, CIDR math) and views in `cmd/util/views.go`, so `mk8s` can reuse them.
- Unit tests run every command against `fake_test.go`, an in-memory API, in table, agent JSON and interactive modes. Contract tests use `tests/contract/mockapi/networks.go`; integration tests live in `tests/integration/network_test.go`.
