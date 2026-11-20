# verda mk8s -- Managed Kubernetes (experimental)

> Experimental: the managed Kubernetes API may change in incompatible ways. Commands, flags and output may change without a deprecation period.

`verda mk8s` (aliases `k8s`, `kubernetes`) manages clusters, their node pools (the worker VMs) and their kubeconfigs. Flags and all options: `verda mk8s <command> --help`.

| Command | What it does |
|---|---|
| `locations` | Locations where this project can create clusters |
| `cluster list \| describe \| create \| upgrade \| delete` | Clusters |
| `node-pool list \| describe \| create \| scale \| delete` (aliases `pool`, `np`) | Node pools: workers of one instance type |
| `kubeconfig list \| describe \| create \| save \| delete` | Client certificates for `kubectl` |

## Workflow

mk8s never creates the private network or the subnet. Create them first with `verda network`, in the cluster's location, and pass their IDs.

```bash
verda mk8s locations
NETWORK_ID=$(verda network create --name k8s -o json | jq -r .id)
SUBNET_ID=$(verda network subnet create --network "$NETWORK_ID" -l FIN-03 -o json | jq -r .id)

verda mk8s cluster create --name prod -l FIN-03 --kubernetes-version 1.37.0 \
  --network "$NETWORK_ID" --subnet "$SUBNET_ID" --wait
verda mk8s node-pool create --cluster prod --name workers --instance-type 1V100.6V \
  --ssh-key "$KEY_ID" --replicas 2 --wait
verda mk8s kubeconfig create --cluster prod --common-name alice --admin

export KUBECONFIG=~/.kube/verda-prod.alice.yaml
kubectl get nodes

verda mk8s node-pool scale workers --cluster prod --replicas 0 --yes
verda mk8s cluster delete prod --yes        # node pools, worker VMs and kubeconfigs go with it
```

`cluster list` shows the endpoint without `https://` and drops the ENDPOINT column when a row would be wider than 120 columns; `-o json|yaml` and `cluster describe` always show it in full.

Cluster and node pool names are unique in the project, so `--cluster prod` is enough: the CLI finds the location with one list call. `-l` gives the location and skips that call. Node pool names must be unique in the project, not only in their cluster. Kubeconfigs are named by an ID that the API chooses (`kubeconfig list`).

## Prompts and `--yes`

A command prompts only on a terminal, with table output, without `--agent`. In scripts, agent mode and `-o json|yaml` it never prompts.

| Command | On a terminal | In scripts, agent mode, `-o json\|yaml` |
|---|---|---|
| `cluster create`, `node-pool create`, `kubeconfig create` | Asks for missing values, shows a summary, asks to confirm | Required flags must be given; no `--yes` needed |
| `node-pool scale` up | Shows a summary, asks to confirm | No `--yes` needed |
| `node-pool scale` down (to 0 too), all deletes, `cluster upgrade` | Red warning, asks to confirm (`--yes` skips it) | `--yes` required, else `CONFIRMATION_REQUIRED` |
| Any command without its cluster, node pool or kubeconfig | Picker | `--cluster` and the positional target are required |

In the create wizards the instance types are listed smallest first (CPU-only types, then by GPU count), because the availability data has no prices. `kubeconfig create` checks `--duration` (at least `1h`) when you type it.

## Waiting

Changes run in the background after the API accepts them. Without `--wait` a command reports `accepted`; with `--wait` it polls and reports `completed`.

| Command | Done when | `--wait-timeout` default |
|---|---|---|
| `cluster create --wait` | phase `running` and an endpoint is set | 15m |
| `node-pool create --wait` | state `active`, all requested workers ready | 15m |
| `node-pool scale --wait` | state `active`, new count reached and ready | 15m |
| `kubeconfig create` (always waits) | status `ready` | 3m |

- A timeout exits non-zero; the change keeps going in the background. Ctrl+C stops only the wait.
- A node pool keeps trying to create workers after a timeout, and workers that exist bill even before they join. Stop a create with `node-pool delete`, a scale-up with `node-pool scale` back to the previous count (the printed advice includes `--yes`, because that is a scale-down).
- `node-pool scale` to the current count sends nothing: "already has N replicas; nothing to do." ("1 replica" for one), status `unchanged` in `-o json|yaml`. With `--wait`, a pool that is not yet active with all workers ready is waited for instead (status `completed`, or a timeout), so a retry after a scale-up timeout does not report success early.
- Deletes and `cluster upgrade` have no `--wait`. A deleted cluster or pool leaves the lists at once while its VMs are still being deleted. The API reports the requested Kubernetes version, not the running one: follow an upgrade with `cluster describe`.

## Kubeconfigs

The kubeconfig YAML holds a client private key.

- It is written only to a file with mode 0600: by default `~/.kube/verda-<cluster>.<common-name>.yaml` (`~/.kube` is created with mode 0700). Never to `-o json|yaml`, `describe` or `--debug`.
- `kubeconfig create` checks the file before it issues a certificate. An existing file stops it unless `--force`; a symlink or a directory is always refused.
- `--file -` writes only the YAML to stdout, for a pipe. It cannot be combined with `--agent` or `-o json|yaml`. A file you redirect it to gets your umask, not 0600.
- In scripts, give `--common-name` and `--admin` (group `system:masters`, full control) and/or `--group` (repeatable).
- Certificates cannot be revoked before they expire (90 days unless `--duration`, at least `1h`): use a short `--duration` for admin credentials. `kubeconfig delete` does not disable copies already downloaded, which keep working until they expire. Deleting the cluster invalidates them. A copy that does not use `system:masters` can also be cut off by removing its group's RBAC binding in the cluster.
- `kubeconfig create --force` replaces the file but not the earlier kubeconfig on the server, which is a different, still valid certificate: the CLI says so on stderr. Run `kubeconfig list` and delete what you no longer need. `kubeconfig save --force` issues no certificate and prints no such hint.
- If `kubeconfig create` stops before the kubeconfig is ready, or cannot write the file, the error gives the kubeconfig ID and the `verda mk8s kubeconfig save ...` command to run. Running `create` again issues another certificate.

## When a create fails

A create is sent once and never retried. If `cluster create`, `node-pool create` or `kubeconfig create` fails with a 5xx, a 408, a timeout or a network error, the object may exist anyway: the CLI says so on stderr (agent mode: in `error.details.warning`). Check the matching `list` before you run `create` again. A 4xx means nothing was created.

## Agent mode

- Missing flag or target: `MISSING_REQUIRED_FLAGS` (exit 2). `kubeconfig create` without `--admin` or `--group` lists them in `details.one_of`.
- Delete, scale-down or upgrade without `--yes`: `CONFIRMATION_REQUIRED` (exit 2).
- Bad flag values or combinations (`--file -` with `--agent`, `--wait --wait-timeout 0`, negative `--replicas`) and a kubeconfig file that cannot be used (exists without `--force`, symlink, directory, missing or read-only directory): `VALIDATION_ERROR` (exit 2).
- Unknown cluster name: `NOT_FOUND` (exit 5). Other API errors show the API message: 400 and 409 are `API_ERROR` (exit 4).
- Managed Kubernetes not enabled for the project: `AUTH_ERROR` (exit 3) with "Managed Kubernetes is not enabled for this project."
- Results: creates `{"status": "accepted"|"completed", "cluster"|"pool": {...}}`; `node-pool scale` adds `previous_replicas` (and can be `"unchanged"`), `cluster upgrade` adds `previous_version`; deletes `{"resource", "name", "location", "action": "delete", "status": "accepted"}` (`cluster` and, for kubeconfigs, `id` too); kubeconfig `create`/`save` print the metadata with `file`, never the YAML. When a `--wait` fails, stdout still has the result, with `wait_error`, and the command exits non-zero.
- A failed `kubeconfig create` wait or write adds `kubeconfig_id`, `save_command` and `warning` to `error.details`.
