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

package serverless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type batchjobUpdateOptions struct {
	Image string

	Compute     string
	ComputeSize int

	RegistryCreds  string
	RegistryPublic bool

	Port       int
	Env        []string
	EnvSecret  []string
	Entrypoint []string
	Cmd        []string

	MaxReplicas int
	Deadline    time.Duration
	RequestTTL  time.Duration

	SecretMounts []string

	Yes bool
}

func newCmdBatchjobUpdate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &batchjobUpdateOptions{}

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a serverless batch-job deployment",
		Long: cmdutil.LongDesc(`
			Update an existing batch-job deployment. Only the specified flags
			are changed; everything else is preserved. The current deployment
			is fetched, changes are merged, and a single PATCH is sent.

			When --env or --env-secret is set, it replaces that type of
			environment variable. The other type is preserved unless also set.
		`),
		Example: cmdutil.Examples(`
			# Change image
			verda batchjob update my-job --image ghcr.io/me/embedder:v2.0

			# Increase deadline and max replicas
			verda batchjob update my-job --deadline 1h --max-replicas 10

			# Change compute
			verda batchjob update my-job --compute RTX6000Ada --compute-size 2
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBatchjobUpdate(cmd, f, ioStreams, opts, args)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Image, flagImage, "", "New container image (must not be ':latest')")
	flags.StringVar(&opts.Compute, flagCompute, "", "Compute resource name")
	flags.IntVar(&opts.ComputeSize, flagComputeSize, 0, "Number of GPUs or vCPU cores per replica")
	flags.StringVar(&opts.RegistryCreds, flagRegistryCreds, "", "Registry credentials name")
	flags.BoolVar(&opts.RegistryPublic, flagRegistryPublic, false, "Pull image anonymously")
	flags.IntVar(&opts.Port, flagPort, 0, "Exposed HTTP port")
	flags.StringArrayVar(&opts.Env, flagEnv, nil, "Plain env KEY=VALUE (replaces all plain vars); repeat for multiple")
	flags.StringArrayVar(&opts.EnvSecret, flagEnvSecret, nil, "Secret env KEY=SECRET_NAME (replaces all secret vars); repeat for multiple")
	flags.StringArrayVar(&opts.Entrypoint, flagEntrypoint, nil, "Override image ENTRYPOINT; repeat for multiple args")
	flags.StringArrayVar(&opts.Cmd, flagCmd, nil, "Override image CMD; repeat for multiple args")
	flags.IntVar(&opts.MaxReplicas, flagMaxReplicas, 0, "Maximum worker replica count")
	flags.DurationVar(&opts.Deadline, flagDeadline, 0, "Per-request deadline (> 0)")
	flags.DurationVar(&opts.RequestTTL, flagRequestTTL, 0, "How long a pending request may live")
	flags.StringArrayVar(&opts.SecretMounts, flagSecretMount, nil, "Secret mount SECRET:MOUNT_PATH; repeat for multiple")
	flags.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation (required in agent mode)")

	cmd.MarkFlagsMutuallyExclusive(flagRegistryCreds, flagRegistryPublic)

	return cmd
}

// -- Flag groups --

var batchjobUpdateAllFlags = []string{
	flagImage, flagCompute, flagComputeSize,
	flagRegistryCreds, flagRegistryPublic,
	flagPort, flagEnv, flagEnvSecret, flagEntrypoint, flagCmd, flagSecretMount,
	flagMaxReplicas, flagDeadline, flagRequestTTL,
}

var batchjobContainerFlags = []string{
	flagImage, flagPort, flagEnv, flagEnvSecret, flagEntrypoint, flagCmd, flagSecretMount,
}

var batchjobScalingUpdateFlags = []string{
	flagMaxReplicas, flagDeadline, flagRequestTTL,
}

// -- Run --

func runBatchjobUpdate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *batchjobUpdateOptions, args []string) error {
	name, err := resolveBatchjobName(cmd, f, ioStreams, args)
	if err != nil || name == "" {
		return err
	}

	if !anyFlagChanged(cmd, batchjobUpdateAllFlags...) {
		return cmdutil.UsageErrorf(cmd, "at least one update flag is required")
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	fetchCtx, fetchCancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer fetchCancel()

	current, err := cmdutil.WithSpinner(fetchCtx, f.Status(), "Loading deployment...", func(ctx context.Context) (*verda.JobDeployment, error) {
		return client.ServerlessJobs.GetJobDeploymentByName(ctx, name)
	})
	if err != nil {
		return fmt.Errorf("fetching deployment: %w", err)
	}

	req, err := buildBatchjobUpdateRequest(cmd, opts, current)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Update request:", req)

	if !f.AgentMode() && !opts.Yes {
		renderBatchjobUpdateSummary(ioStreams.ErrOut, name, cmd, opts, current)
		confirmed, cerr := f.Prompter().Confirm(cmd.Context(), fmt.Sprintf("Update %s?", name))
		if cerr != nil && !isPromptCancel(cerr) {
			return cerr
		}
		if cerr != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	updateCtx, updateCancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer updateCancel()

	deployment, err := cmdutil.WithSpinner(updateCtx, f.Status(), "Updating deployment...", func(ctx context.Context) (*verda.JobDeployment, error) {
		return client.ServerlessJobs.UpdateJobDeployment(ctx, name, req)
	})
	if err != nil {
		return err
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), deployment); wrote {
		return werr
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Updated batch-job deployment %q\n", deployment.Name)
	return nil
}

// -- Request builder --

func buildBatchjobUpdateRequest(cmd *cobra.Command, opts *batchjobUpdateOptions, current *verda.JobDeployment) (*verda.UpdateJobDeploymentRequest, error) {
	req := &verda.UpdateJobDeploymentRequest{}

	if cmd.Flags().Changed(flagCompute) || cmd.Flags().Changed(flagComputeSize) {
		compute := computeFromCurrent(current.Compute)
		if cmd.Flags().Changed(flagCompute) {
			compute.Name = opts.Compute
		}
		if cmd.Flags().Changed(flagComputeSize) {
			if opts.ComputeSize < 1 {
				return nil, errors.New("--compute-size must be >= 1")
			}
			compute.Size = opts.ComputeSize
		}
		req.Compute = &compute
	}

	if cmd.Flags().Changed(flagRegistryCreds) || cmd.Flags().Changed(flagRegistryPublic) {
		if opts.RegistryPublic {
			req.ContainerRegistrySettings = &verda.ContainerRegistrySettings{IsPrivate: false}
		} else {
			req.ContainerRegistrySettings = &verda.ContainerRegistrySettings{
				IsPrivate:   true,
				Credentials: &verda.RegistryCredentialsRef{Name: opts.RegistryCreds},
			}
		}
	}

	if anyFlagChanged(cmd, batchjobContainerFlags...) {
		if len(current.Containers) == 0 {
			return nil, errors.New("deployment has no containers to update")
		}
		c, err := mergeBatchjobContainerUpdate(cmd, opts, &current.Containers[0])
		if err != nil {
			return nil, err
		}
		req.Containers = []verda.CreateDeploymentContainer{c}
	}

	if anyFlagChanged(cmd, batchjobScalingUpdateFlags...) {
		scaling, err := mergeBatchjobScalingUpdate(cmd, opts, current.Scaling)
		if err != nil {
			return nil, err
		}
		req.Scaling = scaling
	}

	return req, nil
}

// -- Container-level merge --

func mergeBatchjobContainerUpdate(cmd *cobra.Command, opts *batchjobUpdateOptions, current *verda.DeploymentContainer) (verda.CreateDeploymentContainer, error) {
	c := verda.CreateDeploymentContainer{
		Name:                current.Name,
		Image:               current.Image.Image,
		ExposedPort:         current.ExposedPort,
		EntrypointOverrides: current.EntrypointOverrides,
		Env:                 current.Env,
		VolumeMounts:        current.VolumeMounts,
	}

	if cmd.Flags().Changed(flagImage) {
		if err := rejectLatestTag(opts.Image); err != nil {
			return c, err
		}
		c.Image = opts.Image
	}
	if cmd.Flags().Changed(flagPort) {
		if opts.Port < 1 || opts.Port > 65535 {
			return c, errors.New("--port must be in 1..65535")
		}
		c.ExposedPort = opts.Port
	}
	if cmd.Flags().Changed(flagEnv) || cmd.Flags().Changed(flagEnvSecret) {
		envs, err := mergeEnvVarsForUpdate(
			cmd.Flags().Changed(flagEnv), cmd.Flags().Changed(flagEnvSecret),
			opts.Env, opts.EnvSecret, current.Env,
		)
		if err != nil {
			return c, err
		}
		c.Env = envs
	}
	if cmd.Flags().Changed(flagEntrypoint) || cmd.Flags().Changed(flagCmd) {
		c.EntrypointOverrides = mergeEntrypointForUpdate(
			cmd.Flags().Changed(flagEntrypoint), cmd.Flags().Changed(flagCmd),
			opts.Entrypoint, opts.Cmd, current.EntrypointOverrides,
		)
	}
	if cmd.Flags().Changed(flagSecretMount) {
		mounts, err := mergeVolumeMountsForUpdate(opts.SecretMounts, current.VolumeMounts)
		if err != nil {
			return c, err
		}
		c.VolumeMounts = mounts
	}

	return c, nil
}

// -- Scaling merge --

func mergeBatchjobScalingUpdate(cmd *cobra.Command, opts *batchjobUpdateOptions, current *verda.JobScalingOptions) (*verda.JobScalingOptions, error) {
	s := &verda.JobScalingOptions{}
	if current != nil {
		*s = *current
	}

	if cmd.Flags().Changed(flagMaxReplicas) {
		if opts.MaxReplicas < 1 {
			return nil, errors.New("--max-replicas must be >= 1")
		}
		s.MaxReplicaCount = opts.MaxReplicas
	}
	if cmd.Flags().Changed(flagDeadline) {
		if opts.Deadline <= 0 {
			return nil, errors.New("--deadline must be > 0")
		}
		s.DeadlineSeconds = int(opts.Deadline.Seconds())
	}
	if cmd.Flags().Changed(flagRequestTTL) {
		s.QueueMessageTTLSeconds = int(opts.RequestTTL.Seconds())
	}

	return s, nil
}

// -- Summary --

func renderBatchjobUpdateSummary(w io.Writer, name string, cmd *cobra.Command, opts *batchjobUpdateOptions, current *verda.JobDeployment) {
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)

	_, _ = fmt.Fprintf(w, "\n  %s %s\n", header.Render("Updating"), name)

	kv := func(k, v string) {
		_, _ = fmt.Fprintf(w, "  %-22s %s\n", label.Render(k), v)
	}

	if cmd.Flags().Changed(flagImage) {
		kv("Image", opts.Image)
	}
	if cmd.Flags().Changed(flagCompute) || cmd.Flags().Changed(flagComputeSize) {
		c := computeFromCurrent(current.Compute)
		if cmd.Flags().Changed(flagCompute) {
			c.Name = opts.Compute
		}
		if cmd.Flags().Changed(flagComputeSize) {
			c.Size = opts.ComputeSize
		}
		kv("Compute", fmt.Sprintf("%s x%d", c.Name, c.Size))
	}
	if cmd.Flags().Changed(flagRegistryCreds) {
		kv("Registry creds", opts.RegistryCreds)
	}
	if cmd.Flags().Changed(flagRegistryPublic) {
		kv("Registry", "public")
	}
	if cmd.Flags().Changed(flagPort) {
		kv("Port", strconv.Itoa(opts.Port))
	}
	if cmd.Flags().Changed(flagEnv) {
		kv("Env (plain)", strconv.Itoa(len(opts.Env)))
	}
	if cmd.Flags().Changed(flagEnvSecret) {
		kv("Env (secret)", strconv.Itoa(len(opts.EnvSecret)))
	}
	if cmd.Flags().Changed(flagSecretMount) {
		kv("Secret mounts", strconv.Itoa(len(opts.SecretMounts)))
	}
	if cmd.Flags().Changed(flagMaxReplicas) {
		kv("Max replicas", strconv.Itoa(opts.MaxReplicas))
	}
	if cmd.Flags().Changed(flagDeadline) {
		kv("Deadline", opts.Deadline.String())
	}
	if cmd.Flags().Changed(flagRequestTTL) {
		kv("Request TTL", opts.RequestTTL.String())
	}

	_, _ = fmt.Fprintln(w)
}
