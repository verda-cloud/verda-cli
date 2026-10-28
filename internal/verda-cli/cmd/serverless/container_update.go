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

type containerUpdateOptions struct {
	Image string

	Spot   bool
	NoSpot bool

	Compute     string
	ComputeSize int

	RegistryCreds  string
	RegistryPublic bool

	Port            int
	HealthcheckOff  bool
	HealthcheckPort int
	HealthcheckPath string
	Env             []string
	EnvSecret       []string
	Entrypoint      []string
	Cmd             []string

	MinReplicas    int
	MaxReplicas    int
	Concurrency    int
	QueuePreset    string
	QueueLoad      int
	CPUUtil        int
	GPUUtil        int
	ScaleUpDelay   time.Duration
	ScaleDownDelay time.Duration
	RequestTTL     time.Duration

	SecretMounts []string

	Yes bool
}

func newCmdContainerUpdate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &containerUpdateOptions{}

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a serverless container deployment",
		Long: cmdutil.LongDesc(`
			Update an existing container deployment. Only the specified flags
			are changed; everything else is preserved. The current state is
			fetched, changes are merged, and one or two PATCHes are sent:
			deployment config and scaling config use separate API endpoints.

			When --env or --env-secret is set, it replaces that type of
			environment variable. The other type is preserved unless also set.
		`),
		Example: cmdutil.Examples(`
			# Change image
			verda container update my-endpoint --image ghcr.io/me/app:v2.0

			# Switch to spot and scale up
			verda container update my-endpoint --spot --max-replicas 10

			# Change compute and concurrency
			verda container update my-endpoint \
			  --compute RTX6000Ada --compute-size 2 --concurrency 4
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContainerUpdate(cmd, f, ioStreams, opts, args)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Image, flagImage, "", "New container image (must not be ':latest')")
	flags.BoolVar(&opts.Spot, flagSpot, false, "Switch to spot compute")
	flags.BoolVar(&opts.NoSpot, flagNoSpot, false, "Switch to on-demand compute")
	flags.StringVar(&opts.Compute, flagCompute, "", "Compute resource name")
	flags.IntVar(&opts.ComputeSize, flagComputeSize, 0, "Number of GPUs or vCPU cores per replica")
	flags.StringVar(&opts.RegistryCreds, flagRegistryCreds, "", "Registry credentials name")
	flags.BoolVar(&opts.RegistryPublic, flagRegistryPublic, false, "Pull image anonymously")
	flags.IntVar(&opts.Port, flagPort, 0, "Exposed HTTP port")
	flags.BoolVar(&opts.HealthcheckOff, flagHealthcheckOff, false, "Disable healthcheck")
	flags.IntVar(&opts.HealthcheckPort, flagHealthcheckPort, 0, "Healthcheck HTTP port")
	flags.StringVar(&opts.HealthcheckPath, flagHealthcheckPath, "", "Healthcheck HTTP path")
	flags.StringArrayVar(&opts.Env, flagEnv, nil, "Plain env KEY=VALUE (replaces all plain vars); repeat for multiple")
	flags.StringArrayVar(&opts.EnvSecret, flagEnvSecret, nil, "Secret env KEY=SECRET_NAME (replaces all secret vars); repeat for multiple")
	flags.StringArrayVar(&opts.Entrypoint, flagEntrypoint, nil, "Override image ENTRYPOINT; repeat for multiple args")
	flags.StringArrayVar(&opts.Cmd, flagCmd, nil, "Override image CMD; repeat for multiple args")
	flags.IntVar(&opts.MinReplicas, flagMinReplicas, 0, "Minimum replica count (0 = scale-to-zero)")
	flags.IntVar(&opts.MaxReplicas, flagMaxReplicas, 0, "Maximum replica count")
	flags.IntVar(&opts.Concurrency, flagConcurrency, 0, "Concurrent requests per replica")
	flags.StringVar(&opts.QueuePreset, flagQueuePreset, "", "Scaling preset: instant | balanced | cost-saver | custom")
	flags.IntVar(&opts.QueueLoad, flagQueueLoad, 0, "Custom queue-load threshold (1..1000)")
	flags.IntVar(&opts.CPUUtil, flagCPUUtil, 0, "CPU utilization trigger % (1..100); 0 = off")
	flags.IntVar(&opts.GPUUtil, flagGPUUtil, 0, "GPU utilization trigger % (1..100); 0 = off")
	flags.DurationVar(&opts.ScaleUpDelay, flagScaleUpDelay, 0, "Delay before scaling up")
	flags.DurationVar(&opts.ScaleDownDelay, flagScaleDownDelay, 0, "Delay before scaling down")
	flags.DurationVar(&opts.RequestTTL, flagRequestTTL, 0, "How long a pending request may live")
	flags.StringArrayVar(&opts.SecretMounts, flagSecretMount, nil, "Secret mount SECRET:MOUNT_PATH; repeat for multiple")
	flags.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation (required in agent mode)")

	cmd.MarkFlagsMutuallyExclusive(flagSpot, flagNoSpot)
	cmd.MarkFlagsMutuallyExclusive(flagRegistryCreds, flagRegistryPublic)
	cmd.MarkFlagsMutuallyExclusive(flagHealthcheckOff, flagHealthcheckPath)
	cmd.MarkFlagsMutuallyExclusive(flagHealthcheckOff, flagHealthcheckPort)

	return cmd
}

// -- Flag groups for detecting changes --

var containerUpdateAllFlags = []string{
	flagImage, flagSpot, flagNoSpot,
	flagCompute, flagComputeSize,
	flagRegistryCreds, flagRegistryPublic,
	flagPort, flagHealthcheckOff, flagHealthcheckPort, flagHealthcheckPath,
	flagEnv, flagEnvSecret, flagEntrypoint, flagCmd, flagSecretMount,
	flagMinReplicas, flagMaxReplicas, flagConcurrency,
	flagQueuePreset, flagQueueLoad, flagCPUUtil, flagGPUUtil,
	flagScaleUpDelay, flagScaleDownDelay, flagRequestTTL,
}

var containerWorkloadFlags = []string{
	flagImage, flagPort, flagHealthcheckOff, flagHealthcheckPort, flagHealthcheckPath,
	flagEnv, flagEnvSecret, flagEntrypoint, flagCmd, flagSecretMount,
}

var containerDeploymentUpdateFlags = []string{
	flagImage, flagSpot, flagNoSpot,
	flagCompute, flagComputeSize,
	flagRegistryCreds, flagRegistryPublic,
	flagPort, flagHealthcheckOff, flagHealthcheckPort, flagHealthcheckPath,
	flagEnv, flagEnvSecret, flagEntrypoint, flagCmd, flagSecretMount,
}

var containerScalingUpdateFlags = []string{
	flagMinReplicas, flagMaxReplicas, flagConcurrency,
	flagQueuePreset, flagQueueLoad, flagCPUUtil, flagGPUUtil,
	flagScaleUpDelay, flagScaleDownDelay, flagRequestTTL,
}

// -- Run --

func runContainerUpdate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *containerUpdateOptions, args []string) error {
	name, err := resolveContainerName(cmd, f, ioStreams, args)
	if err != nil || name == "" {
		return err
	}

	if !anyFlagChanged(cmd, containerUpdateAllFlags...) {
		return cmdutil.UsageErrorf(cmd, "at least one update flag is required")
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	fetchCtx, fetchCancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer fetchCancel()

	needScaling := anyFlagChanged(cmd, containerScalingUpdateFlags...)
	var currentScaling *verda.ContainerScalingOptions

	current, err := cmdutil.WithSpinner(fetchCtx, f.Status(), "Loading deployment...", func(ctx context.Context) (*verda.ContainerDeployment, error) {
		d, derr := client.ContainerDeployments.GetDeploymentByName(ctx, name)
		if derr != nil {
			return nil, derr
		}
		if needScaling {
			s, serr := client.ContainerDeployments.GetDeploymentScaling(ctx, name)
			if serr != nil {
				return nil, fmt.Errorf("fetching scaling: %w", serr)
			}
			currentScaling = s
		}
		return d, nil
	})
	if err != nil {
		return fmt.Errorf("fetching deployment: %w", err)
	}

	deployReq, scalingReq, err := prepareContainerUpdateRequests(cmd, opts, current, currentScaling)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	if deployReq != nil {
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Update request:", deployReq)
	}
	if scalingReq != nil {
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Scaling update request:", scalingReq)
	}

	if !f.AgentMode() && !opts.Yes {
		renderContainerUpdateSummary(ioStreams.ErrOut, name, cmd, opts, current)
		confirmed, cerr := f.Prompter().Confirm(cmd.Context(), fmt.Sprintf("Update %s?", name))
		if cerr != nil && !isPromptCancel(cerr) {
			return cerr
		}
		if cerr != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	deployment, err := sendContainerUpdates(cmd.Context(), f, client, name, deployReq, scalingReq, current)
	if err != nil {
		return err
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), deployment); wrote {
		return werr
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Updated deployment %q\n", deployment.Name)
	return nil
}

// -- Request preparation --

func prepareContainerUpdateRequests(cmd *cobra.Command, opts *containerUpdateOptions, current *verda.ContainerDeployment, currentScaling *verda.ContainerScalingOptions) (*verda.UpdateDeploymentRequest, *verda.UpdateScalingOptionsRequest, error) {
	var deployReq *verda.UpdateDeploymentRequest
	if anyFlagChanged(cmd, containerDeploymentUpdateFlags...) {
		var err error
		deployReq, err = buildContainerUpdateRequest(cmd, opts, current)
		if err != nil {
			return nil, nil, err
		}
	}

	var scalingReq *verda.UpdateScalingOptionsRequest
	if anyFlagChanged(cmd, containerScalingUpdateFlags...) {
		var err error
		scalingReq, err = buildContainerScalingUpdateRequest(cmd, opts, currentScaling)
		if err != nil {
			return nil, nil, err
		}
	}

	return deployReq, scalingReq, nil
}

func sendContainerUpdates(ctx context.Context, f cmdutil.Factory, client *verda.Client, name string, deployReq *verda.UpdateDeploymentRequest, scalingReq *verda.UpdateScalingOptionsRequest, current *verda.ContainerDeployment) (*verda.ContainerDeployment, error) {
	updateCtx, updateCancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer updateCancel()

	deployment := current
	if deployReq != nil {
		var err error
		deployment, err = cmdutil.WithSpinner(updateCtx, f.Status(), "Updating deployment...", func(ctx context.Context) (*verda.ContainerDeployment, error) {
			return client.ContainerDeployments.UpdateDeployment(ctx, name, deployReq)
		})
		if err != nil {
			return nil, err
		}
	}

	if scalingReq != nil {
		_, err := cmdutil.WithSpinner(updateCtx, f.Status(), "Updating scaling...", func(ctx context.Context) (*verda.ContainerScalingOptions, error) {
			return client.ContainerDeployments.UpdateDeploymentScaling(ctx, name, scalingReq)
		})
		if err != nil {
			return nil, err
		}
	}

	return deployment, nil
}

// -- Request builder --

func buildContainerUpdateRequest(cmd *cobra.Command, opts *containerUpdateOptions, current *verda.ContainerDeployment) (*verda.UpdateDeploymentRequest, error) {
	req := &verda.UpdateDeploymentRequest{}

	if cmd.Flags().Changed(flagSpot) || cmd.Flags().Changed(flagNoSpot) {
		spot := opts.Spot
		req.IsSpot = &spot
	}

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

	if anyFlagChanged(cmd, containerWorkloadFlags...) {
		if len(current.Containers) == 0 {
			return nil, errors.New("deployment has no containers to update")
		}
		c, err := mergeContainerUpdate(cmd, opts, &current.Containers[0])
		if err != nil {
			return nil, err
		}
		req.Containers = []verda.CreateDeploymentContainer{c}
	}

	return req, nil
}

// -- Container-level merge --

func mergeContainerUpdate(cmd *cobra.Command, opts *containerUpdateOptions, current *verda.DeploymentContainer) (verda.CreateDeploymentContainer, error) {
	c := verda.CreateDeploymentContainer{
		Name:                current.Name,
		Image:               current.Image.Image,
		ExposedPort:         current.ExposedPort,
		Healthcheck:         current.Healthcheck,
		EntrypointOverrides: current.EntrypointOverrides,
		Env:                 current.Env,
		VolumeMounts:        current.VolumeMounts,
		AutoUpdate:          current.AutoUpdate,
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

	c.Healthcheck = mergeHealthcheckUpdate(cmd, opts, c.Healthcheck, c.ExposedPort)

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

func mergeHealthcheckUpdate(cmd *cobra.Command, opts *containerUpdateOptions, current *verda.ContainerHealthcheck, exposedPort int) *verda.ContainerHealthcheck {
	if !cmd.Flags().Changed(flagHealthcheckOff) && !cmd.Flags().Changed(flagHealthcheckPort) && !cmd.Flags().Changed(flagHealthcheckPath) {
		return current
	}
	if opts.HealthcheckOff {
		return nil
	}
	hc := &verda.ContainerHealthcheck{Enabled: true}
	if current != nil && current.Enabled {
		hc.Port = current.Port
		hc.Path = current.Path
	}
	if cmd.Flags().Changed(flagHealthcheckPort) {
		hc.Port = opts.HealthcheckPort
	}
	if cmd.Flags().Changed(flagHealthcheckPath) {
		hc.Path = opts.HealthcheckPath
	}
	if hc.Port == 0 {
		hc.Port = exposedPort
	}
	return hc
}

// -- Scaling merge --

func buildContainerScalingUpdateRequest(cmd *cobra.Command, opts *containerUpdateOptions, current *verda.ContainerScalingOptions) (*verda.UpdateScalingOptionsRequest, error) {
	req := &verda.UpdateScalingOptionsRequest{}

	minReplicas, maxReplicas := 0, 1
	if current != nil {
		minReplicas = current.MinReplicaCount
		maxReplicas = current.MaxReplicaCount
	}

	if cmd.Flags().Changed(flagMinReplicas) {
		if opts.MinReplicas < 0 {
			return nil, errors.New("--min-replicas must be >= 0")
		}
		req.MinReplicaCount = &opts.MinReplicas
		minReplicas = opts.MinReplicas
	}
	if cmd.Flags().Changed(flagMaxReplicas) {
		if opts.MaxReplicas < 1 {
			return nil, errors.New("--max-replicas must be >= 1")
		}
		req.MaxReplicaCount = &opts.MaxReplicas
		maxReplicas = opts.MaxReplicas
	}
	if maxReplicas < minReplicas {
		return nil, errors.New("--max-replicas must be >= --min-replicas")
	}
	if cmd.Flags().Changed(flagConcurrency) {
		if opts.Concurrency < 1 {
			return nil, errors.New("--concurrency must be >= 1")
		}
		req.ConcurrentRequestsPerReplica = &opts.Concurrency
	}
	if anyFlagChanged(cmd, flagQueuePreset, flagQueueLoad, flagCPUUtil, flagGPUUtil) {
		var currentTriggers *verda.ScalingTriggers
		if current != nil {
			currentTriggers = current.ScalingTriggers
		}
		triggers, err := mergeScalingTriggers(cmd, opts, currentTriggers)
		if err != nil {
			return nil, err
		}
		req.ScalingTriggers = triggers
	}
	if cmd.Flags().Changed(flagScaleUpDelay) {
		delay := int(opts.ScaleUpDelay.Seconds())
		req.ScaleUpPolicy = &verda.ScalingPolicy{DelaySeconds: delay}
	}
	if cmd.Flags().Changed(flagScaleDownDelay) {
		delay := int(opts.ScaleDownDelay.Seconds())
		req.ScaleDownPolicy = &verda.ScalingPolicy{DelaySeconds: delay}
	}
	if cmd.Flags().Changed(flagRequestTTL) {
		ttl := int(opts.RequestTTL.Seconds())
		req.QueueMessageTTLSeconds = &ttl
	}

	return req, nil
}

func mergeScalingTriggers(cmd *cobra.Command, opts *containerUpdateOptions, current *verda.ScalingTriggers) (*verda.ScalingTriggers, error) {
	t := &verda.ScalingTriggers{}
	if current != nil {
		*t = *current
	}

	if cmd.Flags().Changed(flagQueuePreset) || cmd.Flags().Changed(flagQueueLoad) {
		queueLoad, err := resolveQueueLoad(opts.QueuePreset, opts.QueueLoad)
		if err != nil {
			return nil, err
		}
		t.QueueLoad = &verda.QueueLoadTrigger{Threshold: float64(queueLoad)}
	}
	if cmd.Flags().Changed(flagCPUUtil) {
		if opts.CPUUtil < 0 || opts.CPUUtil > 100 {
			return nil, errors.New("--cpu-util must be in 0..100")
		}
		if opts.CPUUtil > 0 {
			t.CPUUtilization = &verda.UtilizationTrigger{Enabled: true, Threshold: opts.CPUUtil}
		} else {
			t.CPUUtilization = nil
		}
	}
	if cmd.Flags().Changed(flagGPUUtil) {
		if opts.GPUUtil < 0 || opts.GPUUtil > 100 {
			return nil, errors.New("--gpu-util must be in 0..100")
		}
		if opts.GPUUtil > 0 {
			t.GPUUtilization = &verda.UtilizationTrigger{Enabled: true, Threshold: opts.GPUUtil}
		} else {
			t.GPUUtilization = nil
		}
	}

	return t, nil
}

// -- Summary --

func renderContainerUpdateSummary(w io.Writer, name string, cmd *cobra.Command, opts *containerUpdateOptions, current *verda.ContainerDeployment) {
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)

	_, _ = fmt.Fprintf(w, "\n  %s %s\n", header.Render("Updating"), name)

	kv := func(k, v string) {
		_, _ = fmt.Fprintf(w, "  %-22s %s\n", label.Render(k), v)
	}

	renderContainerConfigChanges(cmd, opts, current, kv)
	renderContainerScalingChanges(cmd, opts, kv)

	_, _ = fmt.Fprintln(w)
}

func renderContainerConfigChanges(cmd *cobra.Command, opts *containerUpdateOptions, current *verda.ContainerDeployment, kv func(string, string)) {
	if cmd.Flags().Changed(flagImage) {
		kv("Image", opts.Image)
	}
	if cmd.Flags().Changed(flagSpot) || cmd.Flags().Changed(flagNoSpot) {
		billing := computeTypeOnDemand
		if opts.Spot {
			billing = computeTypeSpot
		}
		kv("Billing", billing)
	}
	if cmd.Flags().Changed(flagCompute) || cmd.Flags().Changed(flagComputeSize) {
		c := computeFromCurrent(current.Compute)
		if cmd.Flags().Changed(flagCompute) {
			c.Name = opts.Compute
		}
		if cmd.Flags().Changed(flagComputeSize) {
			c.Size = opts.ComputeSize
		}
		kv("Compute", c.Name+" x"+strconv.Itoa(c.Size))
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
	if cmd.Flags().Changed(flagHealthcheckOff) {
		kv("Healthcheck", "disabled")
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
}

func renderContainerScalingChanges(cmd *cobra.Command, opts *containerUpdateOptions, kv func(string, string)) {
	if cmd.Flags().Changed(flagMinReplicas) {
		kv("Min replicas", strconv.Itoa(opts.MinReplicas))
	}
	if cmd.Flags().Changed(flagMaxReplicas) {
		kv("Max replicas", strconv.Itoa(opts.MaxReplicas))
	}
	if cmd.Flags().Changed(flagConcurrency) {
		kv("Concurrency", strconv.Itoa(opts.Concurrency)+" req/replica")
	}
	if cmd.Flags().Changed(flagQueueLoad) {
		kv("Queue-load", "custom ("+strconv.Itoa(opts.QueueLoad)+")")
	} else if cmd.Flags().Changed(flagQueuePreset) {
		kv("Queue-load preset", opts.QueuePreset)
	}
	if cmd.Flags().Changed(flagCPUUtil) {
		kv("CPU util trigger", strconv.Itoa(opts.CPUUtil)+"%")
	}
	if cmd.Flags().Changed(flagGPUUtil) {
		kv("GPU util trigger", strconv.Itoa(opts.GPUUtil)+"%")
	}
	if cmd.Flags().Changed(flagScaleUpDelay) {
		kv("Scale-up delay", opts.ScaleUpDelay.String())
	}
	if cmd.Flags().Changed(flagScaleDownDelay) {
		kv("Scale-down delay", opts.ScaleDownDelay.String())
	}
	if cmd.Flags().Changed(flagRequestTTL) {
		kv("Request TTL", opts.RequestTTL.String())
	}
}
