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
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// batchjobCreateOptions is the simpler sibling of containerCreateOptions: no
// spot flag (jobs never run on spot), no continuous-scaling parameters, and a
// required deadline. Otherwise mirrors the container shape.
type batchjobCreateOptions struct {
	Name  string
	Image string

	Compute     string
	ComputeSize int

	RegistryCreds string

	Port       int
	Env        []string
	EnvSecret  []string
	Entrypoint []string
	Cmd        []string

	MaxReplicas int
	Deadline    time.Duration
	RequestTTL  time.Duration

	SecretMounts       []string
	GeneralStorageSize int
	SHMSize            int

	Yes bool
}

func newCmdBatchjobCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &batchjobCreateOptions{
		Port:               defaultExposedPort,
		MaxReplicas:        defaultMaxReplicas,
		RequestTTL:         defaultRequestTTL,
		GeneralStorageSize: defaultGeneralStorageGiB,
		SHMSize:            defaultSHMMiB,
	}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a serverless batch-job deployment",
		Long: cmdutil.LongDesc(`
			Create a serverless batch-job deployment. Jobs accept queued
			requests and run each to completion within a deadline. Batch jobs
			cannot use spot compute; --deadline is required.
		`),
		Example: cmdutil.Examples(`
			verda serverless batchjob create \
			  --name nightly-embed \
			  --image ghcr.io/me/embedder:v1 \
			  --compute RTX4500Ada --compute-size 1 \
			  --deadline 30m
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBatchjobCreate(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Deployment name (URL slug; immutable after create)")
	flags.StringVar(&opts.Image, "image", "", "Container image reference (must not be ':latest')")
	flags.StringVar(&opts.Compute, "compute", "", "Compute resource name (e.g. RTX4500Ada, CPUNode)")
	flags.IntVar(&opts.ComputeSize, "compute-size", 1, "Number of GPUs or vCPU cores per replica")
	flags.StringVar(&opts.RegistryCreds, "registry-creds", "", "Registry credentials name (empty = public)")

	flags.IntVar(&opts.Port, "port", opts.Port, "Exposed HTTP port")
	flags.StringArrayVar(&opts.Env, "env", nil, "Environment variable KEY=VALUE; repeat for multiple")
	flags.StringArrayVar(&opts.EnvSecret, "env-secret", nil, "Secret-backed env KEY=SECRET_NAME; repeat for multiple")
	flags.StringArrayVar(&opts.Entrypoint, "entrypoint", nil, "Override image ENTRYPOINT; repeat for multiple args")
	flags.StringArrayVar(&opts.Cmd, "cmd", nil, "Override image CMD; repeat for multiple args")

	flags.IntVar(&opts.MaxReplicas, "max-replicas", opts.MaxReplicas, "Maximum worker replica count")
	flags.DurationVar(&opts.Deadline, "deadline", 0, "Per-request deadline (required; > 0)")
	flags.DurationVar(&opts.RequestTTL, "request-ttl", opts.RequestTTL, "How long a pending request may live before deletion")

	flags.StringArrayVar(&opts.SecretMounts, "secret-mount", nil, "Secret mount SECRET:MOUNT_PATH; repeat for multiple")
	flags.IntVar(&opts.GeneralStorageSize, "general-storage-size", opts.GeneralStorageSize, "Size of the fixed /data mount in GiB")
	flags.IntVar(&opts.SHMSize, "shm-size", opts.SHMSize, "Size of the /dev/shm mount in MiB")

	flags.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation (required in agent mode)")

	return cmd
}

func runBatchjobCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *batchjobCreateOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	if f.AgentMode() {
		if missing := missingBatchjobCreateFlags(opts); len(missing) > 0 {
			return cmdutil.NewMissingFlagsError(missing)
		}
	} else if opts.Name == "" || opts.Image == "" || opts.Compute == "" || opts.Deadline <= 0 {
		if err := runBatchjobWizard(cmd.Context(), f, ioStreams, opts); err != nil {
			return err
		}
	}

	req, err := opts.request()
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

	if !f.AgentMode() && !opts.Yes {
		renderBatchjobSummary(ioStreams.ErrOut, opts)
		confirmed, err := f.Prompter().Confirm(cmd.Context(), fmt.Sprintf("Deploy %s?", opts.Name))
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	deployment, err := cmdutil.WithSpinner(ctx, f.Status(), "Creating batch-job deployment...", func() (*verda.JobDeployment, error) {
		return client.ServerlessJobs.CreateJobDeployment(ctx, req)
	})
	if err != nil {
		return err
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), deployment); wrote {
		return werr
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Created batch-job deployment %q\n", deployment.Name)
	_, _ = fmt.Fprintf(ioStreams.Out, "Endpoint: %s\n", deployment.EndpointBaseURL)
	return nil
}

// runBatchjobWizard drives the batchjob create flow and fills any fields the
// user hasn't pre-set via flags. Shares nine of its ten steps with the
// container wizard; the only batchjob-specific step is the deadline.
func runBatchjobWizard(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *batchjobCreateOptions) error {
	flow := buildBatchjobCreateFlow(ctx, f.VerdaClient, opts)
	engine := wizard.NewEngine(f.Prompter(), f.Status(),
		wizard.WithOutput(ioStreams.ErrOut),
		wizard.WithExitConfirmation())
	return engine.Run(ctx, flow)
}

func missingBatchjobCreateFlags(opts *batchjobCreateOptions) []string {
	var missing []string
	if opts.Name == "" {
		missing = append(missing, "--name")
	}
	if opts.Image == "" {
		missing = append(missing, "--image")
	}
	if opts.Compute == "" {
		missing = append(missing, "--compute")
	}
	if opts.Deadline <= 0 {
		missing = append(missing, "--deadline")
	}
	return missing
}

func (o *batchjobCreateOptions) request() (*verda.CreateJobDeploymentRequest, error) {
	if err := validateDeploymentName(o.Name); err != nil {
		return nil, err
	}
	if err := rejectLatestTag(o.Image); err != nil {
		return nil, err
	}
	if o.ComputeSize < 1 {
		return nil, errors.New("--compute-size must be >= 1")
	}
	if o.MaxReplicas < 1 {
		return nil, errors.New("--max-replicas must be >= 1")
	}
	if o.Deadline <= 0 {
		return nil, errors.New("--deadline must be > 0")
	}
	if o.Port < 1 || o.Port > 65535 {
		return nil, errors.New("--port must be in 1..65535")
	}

	env, err := buildEnvVars(o.Env, o.EnvSecret)
	if err != nil {
		return nil, err
	}
	mounts, err := buildVolumeMounts(o.SecretMounts, o.GeneralStorageSize, o.SHMSize)
	if err != nil {
		return nil, err
	}

	entrypoint := (*verda.ContainerEntrypointOverrides)(nil)
	if len(o.Entrypoint) > 0 || len(o.Cmd) > 0 {
		entrypoint = &verda.ContainerEntrypointOverrides{
			Enabled:    true,
			Entrypoint: append([]string(nil), o.Entrypoint...),
			Cmd:        append([]string(nil), o.Cmd...),
		}
	}

	registry := (*verda.ContainerRegistrySettings)(nil)
	if o.RegistryCreds != "" {
		registry = &verda.ContainerRegistrySettings{
			IsPrivate:   true,
			Credentials: &verda.RegistryCredentialsRef{Name: o.RegistryCreds},
		}
	}

	req := &verda.CreateJobDeploymentRequest{
		Name:                      o.Name,
		ContainerRegistrySettings: registry,
		Compute:                   &verda.ContainerCompute{Name: o.Compute, Size: o.ComputeSize},
		Scaling: &verda.JobScalingOptions{
			MaxReplicaCount:        o.MaxReplicas,
			DeadlineSeconds:        int(o.Deadline.Seconds()),
			QueueMessageTTLSeconds: int(o.RequestTTL.Seconds()),
		},
		Containers: []verda.CreateDeploymentContainer{{
			Image:               o.Image,
			ExposedPort:         o.Port,
			EntrypointOverrides: entrypoint,
			Env:                 env,
			VolumeMounts:        mounts,
		}},
	}

	if err := verda.ValidateCreateJobDeploymentRequest(req); err != nil {
		return nil, err
	}
	return req, nil
}
