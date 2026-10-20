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

package inferno

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	infernoExposedPort     = 5001
	infernoHealthcheckPath = "/health"

	defaultInfernoMaxReplicas    = 1
	defaultInfernoConcurrency    = 1
	defaultInfernoScaleDownDelay = 300 * time.Second
	defaultInfernoRequestTTL     = 300 * time.Second

	defaultGeneralStoragePath = "/data"
)

// infernoDeployOptions holds flags/wizard state for `verda inferno deploy`.
type infernoDeployOptions struct {
	Name  string
	Image string

	Spot bool

	Compute     string
	ComputeSize int

	RegistryCreds  string
	RegistryPublic bool

	Env       []string // KEY=VALUE
	EnvSecret []string // KEY=SECRET_NAME

	MinReplicas    int
	MaxReplicas    int
	Concurrency    int
	QueuePreset    string
	QueueLoad      int
	ScaleUpDelay   time.Duration
	ScaleDownDelay time.Duration
	RequestTTL     time.Duration

	SecretMounts []string // SECRET:PATH

	Yes bool
}

func newCmdInfernoDeploy(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &infernoDeployOptions{
		MinReplicas:    0,
		MaxReplicas:    defaultInfernoMaxReplicas,
		Concurrency:    defaultInfernoConcurrency,
		QueuePreset:    presetInstant,
		ScaleUpDelay:   0,
		ScaleDownDelay: defaultInfernoScaleDownDelay,
		RequestTTL:     defaultInfernoRequestTTL,
	}

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an Inferno worker image to Verda Cloud",
		Long: cmdutil.LongDesc(`
			Deploy an Inferno worker image as a serverless inference endpoint on
			Verda Cloud. The deployment is created with Inferno-specific defaults:
			port 5001, healthcheck at /health, and entrypoint overrides disabled.

			The worker image must already be available in a container registry
			(use 'verda inferno build' to build it, then push to your registry).
		`),
		Example: cmdutil.Examples(`
			# Interactive wizard
			verda inferno deploy

			# Flag-driven
			verda inferno deploy \
			  --name my-model \
			  --image vccr.io/project/my-model-worker:v1.0 \
			  --registry-creds my-creds \
			  --compute "RTX PRO 6000" --compute-size 1

			# With environment variables
			verda inferno deploy \
			  --name flux-schnell \
			  --image docker.io/user/flux-worker:v1.0 \
			  --registry-creds docker-hub \
			  --compute "RTX PRO 6000" \
			  --env HF_HOME=/data/.huggingface \
			  --env HF_TOKEN=hf_abc123
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInfernoDeploy(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Deployment name (URL slug; immutable after create)")
	flags.StringVar(&opts.Image, "image", "", "Worker container image reference (must not be ':latest')")

	flags.BoolVar(&opts.Spot, "spot", false, "Use spot compute instead of on-demand")

	flags.StringVar(&opts.Compute, "compute", "", "Compute resource name (e.g. \"RTX PRO 6000\")")
	flags.IntVar(&opts.ComputeSize, "compute-size", 1, "Number of GPUs per replica")

	flags.StringVar(&opts.RegistryCreds, "registry-creds", "", "Registry credentials name (empty = public)")
	flags.BoolVar(&opts.RegistryPublic, "registry-public", false, "Pull image anonymously (default)")

	flags.StringArrayVar(&opts.Env, "env", nil, "Environment variable KEY=VALUE; repeat for multiple")
	flags.StringArrayVar(&opts.EnvSecret, "env-secret", nil, "Secret-backed env KEY=SECRET_NAME; repeat for multiple")

	flags.IntVar(&opts.MinReplicas, "min-replicas", opts.MinReplicas, "Minimum replica count (0 = scale-to-zero)")
	flags.IntVar(&opts.MaxReplicas, "max-replicas", opts.MaxReplicas, "Maximum replica count")
	flags.IntVar(&opts.Concurrency, "concurrency", opts.Concurrency, "Concurrent requests per replica")
	flags.StringVar(&opts.QueuePreset, "queue-preset", opts.QueuePreset, "Scaling preset: instant | balanced | cost-saver | custom")
	flags.IntVar(&opts.QueueLoad, "queue-load", 0, "Custom queue-load threshold (1..1000); sets --queue-preset=custom")
	flags.DurationVar(&opts.ScaleUpDelay, "scale-up-delay", opts.ScaleUpDelay, "Delay before scaling up")
	flags.DurationVar(&opts.ScaleDownDelay, "scale-down-delay", opts.ScaleDownDelay, "Delay before scaling down")
	flags.DurationVar(&opts.RequestTTL, "request-ttl", opts.RequestTTL, "How long a pending request may live before deletion")

	flags.StringArrayVar(&opts.SecretMounts, "secret-mount", nil, "Secret mount SECRET:MOUNT_PATH; repeat for multiple")

	flags.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation (required in agent mode)")

	return cmd
}

func runInfernoDeploy(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *infernoDeployOptions) error {
	if f.AgentMode() {
		if missing := missingInfernoDeployFlags(opts); len(missing) > 0 {
			return cmdutil.NewMissingFlagsError(missing)
		}
	} else if opts.Name == "" || opts.Image == "" || opts.Compute == "" {
		if err := runInfernoDeployWizard(cmd.Context(), f, ioStreams, opts); err != nil {
			return err
		}
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	req, err := opts.request()
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

	if !f.AgentMode() && !opts.Yes {
		renderInfernoSummary(ioStreams.ErrOut, opts)
		confirmed, err := f.Prompter().Confirm(cmd.Context(), fmt.Sprintf("Deploy %s?", opts.Name))
		if err != nil && !isPromptCancel(err) {
			return err
		}
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	deployment, err := cmdutil.WithSpinner(ctx, f.Status(), "Creating Inferno deployment...", func(ctx context.Context) (*verda.ContainerDeployment, error) {
		return client.ContainerDeployments.CreateDeployment(ctx, req)
	})
	if err != nil {
		return err
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), deployment); wrote {
		return werr
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Created Inferno deployment %q\n", deployment.Name)
	_, _ = fmt.Fprintf(ioStreams.Out, "Endpoint: %s\n", deployment.EndpointBaseURL)
	return nil
}

func runInfernoDeployWizard(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *infernoDeployOptions) error {
	flow := buildInfernoDeployFlow(ctx, f.VerdaClient, opts)
	engine := wizard.NewEngine(f.Prompter(), f.Status(),
		wizard.WithOutput(ioStreams.ErrOut))
	return engine.Run(ctx, flow)
}

func missingInfernoDeployFlags(opts *infernoDeployOptions) []string {
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
	return missing
}

func (o *infernoDeployOptions) request() (*verda.CreateDeploymentRequest, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}

	queueLoad, err := resolveQueueLoad(o.QueuePreset, o.QueueLoad)
	if err != nil {
		return nil, err
	}

	env, err := buildEnvVars(o.Env, o.EnvSecret)
	if err != nil {
		return nil, err
	}

	mounts, err := buildVolumeMounts(o.SecretMounts)
	if err != nil {
		return nil, err
	}

	registry := verda.ContainerRegistrySettings{IsPrivate: false}
	if o.RegistryCreds != "" {
		registry = verda.ContainerRegistrySettings{
			IsPrivate:   true,
			Credentials: &verda.RegistryCredentialsRef{Name: o.RegistryCreds},
		}
	}

	triggers := &verda.ScalingTriggers{
		QueueLoad: &verda.QueueLoadTrigger{Threshold: float64(queueLoad)},
	}

	req := &verda.CreateDeploymentRequest{
		Name: o.Name,
		// TODO(sdk): uncomment once IsInferno is added to verdacloud-sdk-go CreateDeploymentRequest
		// IsInferno: true,
		IsSpot:                    o.Spot,
		Compute:                   verda.ContainerCompute{Name: o.Compute, Size: o.ComputeSize},
		ContainerRegistrySettings: registry,
		Scaling: verda.ContainerScalingOptions{
			MinReplicaCount:              o.MinReplicas,
			MaxReplicaCount:              o.MaxReplicas,
			ScaleDownPolicy:              &verda.ScalingPolicy{DelaySeconds: int(o.ScaleDownDelay.Seconds())},
			ScaleUpPolicy:                &verda.ScalingPolicy{DelaySeconds: int(o.ScaleUpDelay.Seconds())},
			QueueMessageTTLSeconds:       int(o.RequestTTL.Seconds()),
			ConcurrentRequestsPerReplica: o.Concurrency,
			ScalingTriggers:              triggers,
		},
		Containers: []verda.CreateDeploymentContainer{{
			Image:       o.Image,
			ExposedPort: infernoExposedPort,
			Healthcheck: &verda.ContainerHealthcheck{
				Enabled: true,
				Port:    infernoExposedPort,
				Path:    infernoHealthcheckPath,
			},
			EntrypointOverrides: &verda.ContainerEntrypointOverrides{Enabled: false},
			Env:                 env,
			VolumeMounts:        mounts,
		}},
	}

	if err := verda.ValidateCreateDeploymentRequest(req); err != nil {
		return nil, err
	}
	return req, nil
}

func (o *infernoDeployOptions) validate() error {
	if err := validateDeploymentName(o.Name); err != nil {
		return err
	}
	if err := rejectLatestTag(o.Image); err != nil {
		return err
	}
	if o.ComputeSize < 1 {
		return errors.New("--compute-size must be >= 1")
	}
	if o.MinReplicas < 0 {
		return errors.New("--min-replicas must be >= 0")
	}
	if o.MaxReplicas < 1 || o.MaxReplicas < o.MinReplicas {
		return errors.New("--max-replicas must be >= max(1, --min-replicas)")
	}
	if o.Concurrency < 1 {
		return errors.New("--concurrency must be >= 1")
	}
	return nil
}
