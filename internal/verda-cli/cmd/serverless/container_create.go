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
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// Queue-load preset constants. The CLI exposes four named profiles (Instant,
// Balanced, Cost saver, Custom); all four map to an integer threshold written
// into ScalingTriggers.QueueLoad.
const (
	presetInstant   = "instant"
	presetBalanced  = "balanced"
	presetCostSaver = "cost-saver"
	presetCustom    = "custom"

	queueLoadInstant   = 1
	queueLoadBalanced  = 3
	queueLoadCostSaver = 6

	// Fixed storage values — web UI labels these "fixed for now" and does
	// not expose editors. Flags default to these and may be overridden
	// when the API unlocks them.
	defaultGeneralStoragePath = "/data"
	defaultGeneralStorageGiB  = 500
	defaultSHMPath            = "/dev/shm"
	defaultSHMMiB             = 64

	defaultExposedPort     = 80
	defaultHealthcheckPath = "/health"
	defaultMaxReplicas     = 3
	defaultConcurrency     = 1
	defaultScaleDownDelay  = 300 * time.Second
	defaultRequestTTL      = 300 * time.Second
)

// containerCreateOptions collects every field the CreateDeploymentRequest needs.
// Flag parsing populates these; later (follow-up task) the wizard will fill
// remaining gaps. request() turns these into the SDK payload.
type containerCreateOptions struct {
	Name  string
	Image string

	Spot bool

	Compute     string
	ComputeSize int

	RegistryCreds  string // empty = public
	RegistryPublic bool   // explicit --registry-public, just for clarity

	Port            int
	HealthcheckOff  bool
	HealthcheckPort int
	HealthcheckPath string
	Env             []string // KEY=VALUE
	EnvSecret       []string // KEY=SECRET_NAME
	Entrypoint      []string
	Cmd             []string

	MinReplicas    int
	MaxReplicas    int
	Concurrency    int
	QueuePreset    string
	QueueLoad      int // custom override; 0 = use preset
	CPUUtil        int // 0 = off; >0 = enable + threshold
	GPUUtil        int // 0 = off; >0 = enable + threshold
	ScaleUpDelay   time.Duration
	ScaleDownDelay time.Duration
	RequestTTL     time.Duration

	SecretMounts       []string // SECRET:PATH
	GeneralStorageSize int      // GiB; 0 = omit the mount
	SHMSize            int      // MiB; 0 = omit the mount

	Yes bool
}

func newCmdContainerCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &containerCreateOptions{
		Port:               defaultExposedPort,
		HealthcheckPath:    defaultHealthcheckPath,
		MinReplicas:        0,
		MaxReplicas:        defaultMaxReplicas,
		Concurrency:        defaultConcurrency,
		QueuePreset:        presetBalanced,
		ScaleUpDelay:       0,
		ScaleDownDelay:     defaultScaleDownDelay,
		RequestTTL:         defaultRequestTTL,
		GeneralStorageSize: defaultGeneralStorageGiB,
		SHMSize:            defaultSHMMiB,
	}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a serverless container deployment",
		Long: cmdutil.LongDesc(`
			Create an always-on serverless container deployment. Without flags,
			launches an interactive wizard (coming in a follow-up task). With
			flags, builds the deployment request directly and submits it.

			Images must use a specific tag — ":latest" is rejected client-side.
		`),
		Example: cmdutil.Examples(`
			# Minimal flag-driven
			verda serverless container create \
			  --name my-endpoint \
			  --image ghcr.io/ai-dock/comfyui:cpu-22.04 \
			  --compute RTX4500Ada --compute-size 1

			# With env vars and scaling preset
			verda serverless container create \
			  --name my-api --image ghcr.io/me/llm:v1.2 \
			  --compute RTX4500Ada --compute-size 1 \
			  --env HF_HOME=/data/.huggingface \
			  --max-replicas 5 --queue-preset cost-saver
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runContainerCreate(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Deployment name (URL slug; immutable after create)")
	flags.StringVar(&opts.Image, "image", "", "Container image reference (must not be ':latest')")

	flags.BoolVar(&opts.Spot, "spot", false, "Use spot compute instead of on-demand")

	flags.StringVar(&opts.Compute, "compute", "", "Compute resource name (e.g. RTX4500Ada, CPUNode)")
	flags.IntVar(&opts.ComputeSize, "compute-size", 1, "Number of GPUs or vCPU cores per replica")

	flags.StringVar(&opts.RegistryCreds, "registry-creds", "", "Registry credentials name (empty = public)")
	flags.BoolVar(&opts.RegistryPublic, "registry-public", false, "Pull image anonymously (default)")

	flags.IntVar(&opts.Port, "port", opts.Port, "Exposed HTTP port")
	flags.BoolVar(&opts.HealthcheckOff, "healthcheck-off", false, "Disable healthcheck (default: on at /health)")
	flags.IntVar(&opts.HealthcheckPort, "healthcheck-port", 0, "Healthcheck HTTP port (defaults to --port)")
	flags.StringVar(&opts.HealthcheckPath, "healthcheck-path", opts.HealthcheckPath, "Healthcheck HTTP path")
	flags.StringArrayVar(&opts.Env, "env", nil, "Environment variable KEY=VALUE; repeat for multiple")
	flags.StringArrayVar(&opts.EnvSecret, "env-secret", nil, "Secret-backed env KEY=SECRET_NAME; repeat for multiple")
	flags.StringArrayVar(&opts.Entrypoint, "entrypoint", nil, "Override image ENTRYPOINT; repeat for multiple args")
	flags.StringArrayVar(&opts.Cmd, "cmd", nil, "Override image CMD; repeat for multiple args")

	flags.IntVar(&opts.MinReplicas, "min-replicas", opts.MinReplicas, "Minimum replica count (0 = scale-to-zero)")
	flags.IntVar(&opts.MaxReplicas, "max-replicas", opts.MaxReplicas, "Maximum replica count")
	flags.IntVar(&opts.Concurrency, "concurrency", opts.Concurrency, "Concurrent requests per replica")
	flags.StringVar(&opts.QueuePreset, "queue-preset", opts.QueuePreset, "Scaling preset: instant | balanced | cost-saver | custom")
	flags.IntVar(&opts.QueueLoad, "queue-load", 0, "Custom queue-load threshold (1..1000); sets --queue-preset=custom when used")
	flags.IntVar(&opts.CPUUtil, "cpu-util", 0, "CPU utilization trigger threshold % (1..100); 0 = off")
	flags.IntVar(&opts.GPUUtil, "gpu-util", 0, "GPU utilization trigger threshold % (1..100); 0 = off")
	flags.DurationVar(&opts.ScaleUpDelay, "scale-up-delay", opts.ScaleUpDelay, "Delay before scaling up")
	flags.DurationVar(&opts.ScaleDownDelay, "scale-down-delay", opts.ScaleDownDelay, "Delay before scaling down")
	flags.DurationVar(&opts.RequestTTL, "request-ttl", opts.RequestTTL, "How long a pending request may live before deletion")

	flags.StringArrayVar(&opts.SecretMounts, "secret-mount", nil, "Secret mount SECRET:MOUNT_PATH; repeat for multiple")
	flags.IntVar(&opts.GeneralStorageSize, "general-storage-size", opts.GeneralStorageSize, "Size of the fixed /data mount in GiB")
	flags.IntVar(&opts.SHMSize, "shm-size", opts.SHMSize, "Size of the /dev/shm mount in MiB")

	flags.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation (required in agent mode)")

	return cmd
}

func runContainerCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *containerCreateOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	if f.AgentMode() {
		if missing := missingContainerCreateFlags(opts); len(missing) > 0 {
			return cmdutil.NewMissingFlagsError(missing)
		}
	} else if opts.Name == "" || opts.Image == "" || opts.Compute == "" {
		if err := runContainerWizard(cmd.Context(), f, ioStreams, opts); err != nil {
			return err
		}
	}

	req, err := opts.request()
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

	// Interactive confirmation with summary. Agent mode relies on --yes.
	if !f.AgentMode() && !opts.Yes {
		renderContainerSummary(ioStreams.ErrOut, opts)
		confirmed, err := f.Prompter().Confirm(cmd.Context(), fmt.Sprintf("Deploy %s?", opts.Name))
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	deployment, err := cmdutil.WithSpinner(ctx, f.Status(), "Creating container deployment...", func() (*verda.ContainerDeployment, error) {
		return client.ContainerDeployments.CreateDeployment(ctx, req)
	})
	if err != nil {
		return err
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), deployment); wrote {
		return werr
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Created deployment %q\n", deployment.Name)
	_, _ = fmt.Fprintf(ioStreams.Out, "Endpoint: %s\n", deployment.EndpointBaseURL)
	return nil
}

// runContainerWizard drives the interactive 22-step create flow. Writes into
// opts in place; the caller then turns opts into a request via opts.request().
func runContainerWizard(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *containerCreateOptions) error {
	flow := buildContainerCreateFlow(ctx, f.VerdaClient, opts)
	engine := wizard.NewEngine(f.Prompter(), f.Status(),
		wizard.WithOutput(ioStreams.ErrOut),
		wizard.WithExitConfirmation())
	return engine.Run(ctx, flow)
}

func missingContainerCreateFlags(opts *containerCreateOptions) []string {
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

// validate checks every option for well-formed values before the request is
// assembled. Split out from request() to keep cyclomatic complexity low — the
// cluster of range checks lives here, the assembly lives there.
func (o *containerCreateOptions) validate() error {
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
	if o.Port < 1 || o.Port > 65535 {
		return errors.New("--port must be in 1..65535")
	}
	if o.CPUUtil < 0 || o.CPUUtil > 100 {
		return errors.New("--cpu-util must be in 0..100")
	}
	if o.GPUUtil < 0 || o.GPUUtil > 100 {
		return errors.New("--gpu-util must be in 0..100")
	}
	return nil
}

// request assembles a CreateDeploymentRequest from the options. Validation
// happens in validate(); assembly + the SDK's server-side-parity check live here.
func (o *containerCreateOptions) request() (*verda.CreateDeploymentRequest, error) {
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

	mounts, err := buildVolumeMounts(o.SecretMounts, o.GeneralStorageSize, o.SHMSize)
	if err != nil {
		return nil, err
	}

	healthcheck := (*verda.ContainerHealthcheck)(nil)
	if !o.HealthcheckOff {
		hcPort := o.HealthcheckPort
		if hcPort == 0 {
			hcPort = o.Port
		}
		healthcheck = &verda.ContainerHealthcheck{
			Enabled: true,
			Port:    hcPort,
			Path:    o.HealthcheckPath,
		}
	}

	entrypoint := (*verda.ContainerEntrypointOverrides)(nil)
	if len(o.Entrypoint) > 0 || len(o.Cmd) > 0 {
		entrypoint = &verda.ContainerEntrypointOverrides{
			Enabled:    true,
			Entrypoint: append([]string(nil), o.Entrypoint...),
			Cmd:        append([]string(nil), o.Cmd...),
		}
	}

	registry := verda.ContainerRegistrySettings{IsPrivate: false}
	if o.RegistryCreds != "" {
		registry = verda.ContainerRegistrySettings{
			IsPrivate:   true,
			Credentials: &verda.RegistryCredentialsRef{Name: o.RegistryCreds},
		}
	}

	req := &verda.CreateDeploymentRequest{
		Name:                      o.Name,
		IsSpot:                    o.Spot,
		Compute:                   verda.ContainerCompute{Name: o.Compute, Size: o.ComputeSize},
		ContainerRegistrySettings: registry,
		Scaling:                   buildContainerScaling(o, queueLoad),
		Containers: []verda.CreateDeploymentContainer{{
			Image:               o.Image,
			ExposedPort:         o.Port,
			Healthcheck:         healthcheck,
			EntrypointOverrides: entrypoint,
			Env:                 env,
			VolumeMounts:        mounts,
		}},
	}

	if err := verda.ValidateCreateDeploymentRequest(req); err != nil {
		return nil, err
	}
	return req, nil
}

func resolveQueueLoad(preset string, custom int) (int, error) {
	if custom > 0 {
		if custom > 1000 {
			return 0, errors.New("--queue-load must be in 1..1000")
		}
		return custom, nil
	}
	switch strings.ToLower(preset) {
	case presetInstant:
		return queueLoadInstant, nil
	case presetBalanced, "":
		return queueLoadBalanced, nil
	case presetCostSaver, "cost_saver", "costsaver":
		return queueLoadCostSaver, nil
	case presetCustom:
		return 0, errors.New("--queue-preset=custom requires --queue-load")
	default:
		return 0, fmt.Errorf("invalid --queue-preset %q: expected instant, balanced, cost-saver, or custom", preset)
	}
}

func buildEnvVars(plain, secret []string) ([]verda.ContainerEnvVar, error) {
	total := len(plain) + len(secret)
	if total == 0 {
		return nil, nil
	}
	out := make([]verda.ContainerEnvVar, 0, total)
	for _, e := range plain {
		v, err := parseEnvFlag(e, envTypePlain)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	for _, e := range secret {
		v, err := parseEnvFlag(e, envTypeSecret)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func buildVolumeMounts(secretMounts []string, generalStorageGiB, shmMiB int) ([]verda.ContainerVolumeMount, error) {
	var mounts []verda.ContainerVolumeMount
	for _, entry := range secretMounts {
		m, err := parseSecretMountFlag(entry)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, m)
	}
	if generalStorageGiB > 0 {
		mounts = append(mounts, verda.ContainerVolumeMount{
			Type:      mountTypeShared,
			MountPath: defaultGeneralStoragePath,
			SizeInMB:  generalStorageGiB * 1024,
		})
	}
	if shmMiB > 0 {
		mounts = append(mounts, verda.ContainerVolumeMount{
			Type:      mountTypeSHM,
			MountPath: defaultSHMPath,
			SizeInMB:  shmMiB,
		})
	}
	return mounts, nil
}

func buildContainerScaling(o *containerCreateOptions, queueLoad int) verda.ContainerScalingOptions {
	triggers := &verda.ScalingTriggers{
		QueueLoad: &verda.QueueLoadTrigger{Threshold: float64(queueLoad)},
	}
	if o.CPUUtil > 0 {
		triggers.CPUUtilization = &verda.UtilizationTrigger{Enabled: true, Threshold: o.CPUUtil}
	}
	if o.GPUUtil > 0 {
		triggers.GPUUtilization = &verda.UtilizationTrigger{Enabled: true, Threshold: o.GPUUtil}
	}
	return verda.ContainerScalingOptions{
		MinReplicaCount:              o.MinReplicas,
		MaxReplicaCount:              o.MaxReplicas,
		ScaleDownPolicy:              &verda.ScalingPolicy{DelaySeconds: int(o.ScaleDownDelay.Seconds())},
		ScaleUpPolicy:                &verda.ScalingPolicy{DelaySeconds: int(o.ScaleUpDelay.Seconds())},
		QueueMessageTTLSeconds:       int(o.RequestTTL.Seconds()),
		ConcurrentRequestsPerReplica: o.Concurrency,
		ScalingTriggers:              triggers,
	}
}
