// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mk8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	flagName              = "name"
	flagKubernetesVersion = "kubernetes-version"
	flagNetwork           = "network"
	flagSubnet            = "subnet"
	flagPodCIDR           = "pod-cidr"
	flagServiceCIDR       = "service-cidr"
	flagWait              = "wait"
	flagWaitTimeout       = "wait-timeout"

	defaultPodCIDR            = "10.100.0.0/16"
	defaultServiceCIDR        = "10.200.0.0/16"
	defaultClusterWaitTimeout = 15 * time.Minute
)

type clusterCreateOptions struct {
	Name        string
	Location    string
	Version     string
	Network     string
	Subnet      string
	PodCIDR     string
	ServiceCIDR string
	Wait        cmdutil.WaitOptions

	// The CIDR flags have defaults, so the wizard needs Changed.
	podCIDRSet, serviceCIDRSet bool
}

// ClusterCreateResult is the structured output of `cluster create`: accepted,
// or completed once --wait saw the cluster running with an endpoint.
type ClusterCreateResult struct {
	Status    string      `json:"status"               yaml:"status"`
	Cluster   ClusterView `json:"cluster"              yaml:"cluster"`
	WaitError string      `json:"wait_error,omitempty" yaml:"wait_error,omitempty"`
}

// NewCmdClusterCreate creates `verda mk8s cluster create`.
func NewCmdClusterCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &clusterCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a cluster",
		Long: longDesc(`
			Create a managed Kubernetes cluster. The private network and the
			subnet must exist already: create them with verda network and pass
			their IDs. mk8s never creates them. The CLI sends the IDs as given;
			the API checks them.

			On a terminal, the CLI asks for each value that you do not give,
			then shows a summary and asks for confirmation. In scripts, agent
			mode and -o json|yaml, --name, --location, --kubernetes-version,
			--network and --subnet are required and the command never asks.

			The command returns once the API accepts the request (status
			"accepted"). With --wait it polls until the cluster is running and
			has an endpoint (status "completed"). If the wait times out, or you
			stop it with Ctrl+C, the cluster is still being created.

			A create is sent once and is never retried: if it fails with a
			server or network error, the cluster may exist anyway, so check
			cluster list before you run it again.
		`),
		Example: cmdutil.Examples(`
			# Ask for each value (terminal only)
			verda mk8s cluster create

			# Find the IDs of the private network and of its subnet
			verda network list
			verda network subnet list --network $NETWORK_ID -l FIN-03

			# Create without prompts
			verda mk8s cluster create --name prod -l FIN-03 --kubernetes-version 1.37.0 --network $NETWORK_ID --subnet $SUBNET_ID

			# Other pod and service ranges; wait up to 30 minutes for the cluster to run
			verda mk8s cluster create --name prod -l FIN-03 --kubernetes-version 1.37.0 --network $NETWORK_ID --subnet $SUBNET_ID --pod-cidr 10.64.0.0/16 --service-cidr 10.65.0.0/16 --wait --wait-timeout 30m

			# Agent mode: JSON result and structured errors
			verda --agent mk8s cluster create --name prod -l FIN-03 --kubernetes-version 1.37.0 --network $NETWORK_ID --subnet $SUBNET_ID --wait
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.podCIDRSet = cmd.Flags().Changed(flagPodCIDR)
			opts.serviceCIDRSet = cmd.Flags().Changed(flagServiceCIDR)
			return runClusterCreate(cmd, f, ioStreams, opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.Name, flagName, "", "Cluster name: a DNS label of at most 30 characters, unique in the project")
	addLocationFlag(cmd, &opts.Location, "Location code, for example FIN-03 (see verda mk8s locations)")
	flags.StringVar(&opts.Version, flagKubernetesVersion, "", "Kubernetes version, for example 1.37.0 (the API checks it)")
	flags.StringVar(&opts.Network, flagNetwork, "", "ID of the private network (see verda network list)")
	flags.StringVar(&opts.Subnet, flagSubnet, "", "ID of a subnet of that network in the cluster's location (see verda network subnet list)")
	flags.StringVar(&opts.PodCIDR, flagPodCIDR, defaultPodCIDR, "IPv4 range for pod IPs, /21 or larger")
	flags.StringVar(&opts.ServiceCIDR, flagServiceCIDR, defaultServiceCIDR, "IPv4 range for service IPs; must not overlap the pod range")
	flags.BoolVar(&opts.Wait.Wait, flagWait, false, "Wait until the cluster is running and has an endpoint")
	flags.DurationVar(&opts.Wait.Timeout, flagWaitTimeout, defaultClusterWaitTimeout, waitTimeUsage)
	return cmd
}

func (o *clusterCreateOptions) trim() {
	for _, s := range []*string{&o.Name, &o.Location, &o.Version, &o.Network, &o.Subnet, &o.PodCIDR, &o.ServiceCIDR} {
		*s = strings.TrimSpace(*s)
	}
}

func (o *clusterCreateOptions) missing() []string {
	var m []string
	for _, r := range []struct{ flag, value string }{
		{flagName, o.Name}, {flagLocation, o.Location}, {flagKubernetesVersion, o.Version},
		{flagNetwork, o.Network}, {flagSubnet, o.Subnet},
	} {
		if r.value == "" {
			m = append(m, "--"+r.flag)
		}
	}
	return m
}

func (o *clusterCreateOptions) request() verda.CreateManagedKubernetesClusterRequest {
	return verda.CreateManagedKubernetesClusterRequest{
		Name:              o.Name,
		KubernetesVersion: o.Version,
		PodCIDR:           o.PodCIDR,
		ServiceCIDR:       o.ServiceCIDR,
		Location:          o.Location,
		NetworkID:         o.Network,
		SubnetID:          o.Subnet,
	}
}

func runClusterCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *clusterCreateOptions) error {
	opts.trim()
	if opts.Wait.Wait && opts.Wait.Timeout <= 0 {
		return cmdutil.UsageErrorf(cmd, "--%s must be greater than 0", flagWaitTimeout)
	}
	missing := opts.missing()
	interactive := interactiveTTY(f)
	if len(missing) > 0 && !interactive {
		return cmdutil.NewMissingFlagsError(missing)
	}
	// Complete flags are checked before any call, wizard answers before the confirmation.
	req := opts.request()
	if len(missing) == 0 {
		if err := checkRequest(cmd, kindCluster, req.Validate()); err != nil {
			return err
		}
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		flow := buildClusterCreateFlow(opts, newWizardCache(f, client))
		if err := runWizard(cmd.Context(), f, ioStreams, flow); err != nil {
			return err
		}
		opts.trim()
		req = opts.request()
		if err := checkRequest(cmd, kindCluster, req.Validate()); err != nil {
			return err
		}
	}

	if interactive {
		ok, err := confirmClusterCreate(cmd.Context(), f, ioStreams, &req)
		if err != nil || !ok {
			return err
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)
	cluster, err := apiCall(cmd.Context(), f, "Creating cluster...", func(ctx context.Context) (*verda.ManagedKubernetesCluster, error) {
		return client.ManagedKubernetes.Create(ctx, req)
	})
	if err != nil {
		return createFailed(f, ioStreams, kindCluster, "verda mk8s cluster list", err)
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", newClusterView(cluster))

	result := ClusterCreateResult{Status: statusAccepted, Cluster: newClusterView(cluster)}
	var waitErr error
	if opts.Wait.Wait {
		waitErr = waitForCluster(cmd.Context(), f, ioStreams, client, cluster, opts.Wait.Timeout, &result)
	}

	if written, err := writeResult(f, ioStreams, result, waitErr); written {
		return err
	}
	printClusterCreated(ioStreams, &result)
	return waitErr
}

func confirmClusterCreate(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, req *verda.CreateManagedKubernetesClusterRequest) (bool, error) {
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\nCreate cluster %s\n  Location:      %s\n  Version:       %s\n  Network:       %s\n  Subnet:        %s\n  Pod CIDR:      %s\n  Service CIDR:  %s\n\n",
		req.Name, req.Location, req.KubernetesVersion, req.NetworkID, req.SubnetID, req.PodCIDR, req.ServiceCIDR)
	return askConfirm(ctx, ioStreams, f.Prompter(), "Create cluster "+req.Name+"?")
}

func waitForCluster(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, created *verda.ManagedKubernetesCluster, timeout time.Duration, result *ClusterCreateResult) error {
	name, location := created.Name, created.Location
	if f.OutputFormat() == outputTable {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Cluster %s accepted; waiting until it is running (Ctrl+C stops waiting, the cluster stays).\n", name)
	}
	last, err := waitFor(ctx, f, waitWriter(f, ioStreams), timeout, nil,
		func(ctx context.Context) (*verda.ManagedKubernetesCluster, error) {
			return client.ManagedKubernetes.Get(ctx, location, name)
		}, clusterReady)
	if last != nil {
		result.Cluster = newClusterView(last)
	}
	if err == nil {
		result.Status = statusCompleted
		return nil
	}

	phase := created.Phase
	if last != nil {
		phase = last.Phase
	}
	describe := describeCmd(name, location)
	err = waitFailure(err, &waitFailureInfo{
		Subject:     fmt.Sprintf("cluster %s in %s", name, location),
		Timeout:     timeout,
		LastSeen:    "last phase: " + phase,
		TimeoutTail: fmt.Sprintf("It is still being created; check with `%s`", describe),
		CheckCmd:    describe,
	})
	result.WaitError = err.Error()
	return err
}

func printClusterCreated(ioStreams cmdutil.IOStreams, r *ClusterCreateResult) {
	c := &r.Cluster
	if r.Status == statusCompleted {
		_, _ = fmt.Fprintf(ioStreams.Out, "Cluster %s completed (%s): %s, endpoint %s\n", c.Name, c.Location, c.Phase, c.ExternalEndpoint)
		return
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Cluster %s accepted (%s), phase %s.\n", c.Name, c.Location, c.Phase)
	if r.WaitError == "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Check progress with: %s\n", describeCmd(c.Name, c.Location))
	}
}
