package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

var validSpotPolicies = map[string]struct{}{
	"":                                   {},
	verda.SpotDiscontinueKeepDetached:    {},
	verda.SpotDiscontinueMoveToTrash:     {},
	verda.SpotDiscontinueDeletePermanent: {},
}

type createOptions struct {
	InstanceType string
	Image        string
	Hostname     string
	Description  string
	Kind         string

	SSHKeyIDs       []string
	LocationCode    string
	Contract        string
	Pricing         string
	StartupScriptID string
	ExistingVolumes []string
	VolumeSpecs     []string
	IsSpot          bool
	Coupon          string

	OSVolumeName              string
	OSVolumeSize              int
	OSVolumeOnSpotDiscontinue string
	StorageName               string
	StorageSize               int
	StorageType               string
	StorageOnSpotDiscontinue  string
}

// NewCmdCreate creates the vm create cobra command.
func NewCmdCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &createOptions{
		LocationCode: verda.LocationFIN01,
		StorageType:  verda.VolumeTypeNVMe,
	}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a VM instance",
		Long: cmdutil.LongDesc(`
			Create a Verda VM instance using the official Verda Cloud Go SDK.
		`),
		Example: cmdutil.Examples(`
			verda vm create \
			  --kind gpu \
			  --instance-type 1V100.6V \
			  --location FIN-01 \
			  --os ubuntu-24.04-cuda-12.8-open-docker \
			  --os-volume-size 100 \
			  --hostname gpu-runner \
			  --description "GPU runner for batch jobs" \
			  --ssh-key ssh_key_123

			verda vm create \
			  --kind cpu \
			  --instance-type CPU.4V.16G \
			  --location FIN-03 \
			  --os ubuntu-24.04 \
			  --os-volume-size 55 \
			  --hostname training-node \
			  --is-spot \
			  --storage-size 500
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Kind, "kind", "", "Compute class: gpu or cpu")
	flags.StringVar(&opts.InstanceType, "instance-type", "", "Instance type, for example 1V100.6V")
	flags.StringVar(&opts.InstanceType, "type", "", "Alias of --instance-type")
	flags.StringVar(&opts.Image, "os", "", "OS image slug or an existing detached OS volume ID")
	flags.StringVar(&opts.Image, "image", "", "Alias of --os")
	flags.StringVar(&opts.Hostname, "hostname", "", "Hostname for the new VM")
	flags.StringVar(&opts.Description, "description", "", "Human-readable description; defaults to the hostname")
	flags.StringSliceVar(&opts.SSHKeyIDs, "ssh-key", nil, "SSH key ID to inject into the instance; repeat the flag for multiple keys")
	flags.StringSliceVar(&opts.SSHKeyIDs, "ssh-key-id", nil, "Alias of --ssh-key")
	flags.StringVar(&opts.LocationCode, "location", opts.LocationCode, "Location code, for example FIN-01")
	flags.StringVar(&opts.Contract, "contract", "", "Billing contract: pay_as_go, long_term, or spot. Long-term durations are not exposed by POST /v1/instances.")
	flags.StringVar(&opts.Pricing, "pricing", "", "Pricing mode string accepted by the API, for example FIXED_PRICE")
	flags.StringVar(&opts.StartupScriptID, "startup-script", "", "Startup script ID to attach")
	flags.StringVar(&opts.StartupScriptID, "startup-script-id", "", "Alias of --startup-script")
	flags.StringSliceVar(&opts.ExistingVolumes, "existing-volume", nil, "Existing volume ID to attach; repeat the flag for multiple volumes")
	flags.StringArrayVar(&opts.VolumeSpecs, "volume", nil, "Create and attach a new volume using name:size:type[:location[:on-spot-discontinue]]")
	flags.BoolVar(&opts.IsSpot, "is-spot", false, "Request a spot instance")
	flags.BoolVar(&opts.IsSpot, "spot", false, "Alias of --is-spot")
	flags.StringVar(&opts.Coupon, "coupon", "", "Coupon code to apply to the instance creation")
	flags.StringVar(&opts.OSVolumeName, "os-volume-name", "", "Name of the OS volume to create")
	flags.IntVar(&opts.OSVolumeSize, "os-volume-size", 0, "Size of the OS volume in GiB")
	flags.StringVar(&opts.OSVolumeOnSpotDiscontinue, "os-volume-on-spot-discontinue", "", "Spot discontinue policy for the OS volume: keep_detached, move_to_trash, or delete_permanently")
	flags.StringVar(&opts.StorageName, "storage-name", "", "Name of the optional additional storage volume; defaults to <hostname>-storage")
	flags.IntVar(&opts.StorageSize, "storage-size", 0, "Size of the optional additional storage volume in GiB")
	flags.StringVar(&opts.StorageType, "storage-type", opts.StorageType, "Type of the optional additional storage volume")
	flags.StringVar(&opts.StorageOnSpotDiscontinue, "storage-on-spot-discontinue", "", "Spot discontinue policy for the optional additional storage volume")

	_ = flags.MarkHidden("type")
	_ = flags.MarkHidden("image")
	_ = flags.MarkHidden("ssh-key-id")
	_ = flags.MarkHidden("startup-script-id")
	_ = flags.MarkHidden("spot")

	return cmd
}

func runCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *createOptions) error {
	// Verify credentials are available before doing anything.
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	// If any required field is missing, run the interactive wizard.
	if opts.InstanceType == "" || opts.Image == "" || opts.Hostname == "" {
		if err := runWizard(cmd.Context(), f, ioStreams, opts); err != nil {
			return err
		}
	}

	req, err := opts.request()
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Creating VM instance...")
	}
	instance, err := client.Instances.Create(ctx, req)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(ioStreams.Out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(instance)
}

func runWizard(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *createOptions) error {
	flow := buildCreateFlow(f.VerdaClient, opts)
	engine := wizard.NewEngine(f.Prompter(), f.Status(), wizard.WithOutput(ioStreams.ErrOut))
	return engine.Run(ctx, flow)
}

func (o *createOptions) request() (verda.CreateInstanceRequest, error) {
	contract, err := normalizeContract(o.Contract)
	if err != nil {
		return verda.CreateInstanceRequest{}, err
	}
	if o.IsSpot && contract == "" {
		contract = "SPOT"
	}
	if _, ok := validSpotPolicies[o.OSVolumeOnSpotDiscontinue]; !ok {
		return verda.CreateInstanceRequest{}, fmt.Errorf("invalid --os-volume-on-spot-discontinue %q", o.OSVolumeOnSpotDiscontinue)
	}
	if o.OSVolumeOnSpotDiscontinue != "" && !o.IsSpot {
		return verda.CreateInstanceRequest{}, fmt.Errorf("--os-volume-on-spot-discontinue requires --is-spot")
	}
	if err := validateKind(o.Kind, o.InstanceType); err != nil {
		return verda.CreateInstanceRequest{}, err
	}

	volumes, err := parseVolumeSpecs(o.VolumeSpecs, o.IsSpot)
	if err != nil {
		return verda.CreateInstanceRequest{}, err
	}
	volumes, err = appendStorageVolume(volumes, o)
	if err != nil {
		return verda.CreateInstanceRequest{}, err
	}

	req := verda.CreateInstanceRequest{
		InstanceType:    o.InstanceType,
		Image:           o.Image,
		Hostname:        o.Hostname,
		Description:     o.descriptionValue(),
		SSHKeyIDs:       append([]string(nil), o.SSHKeyIDs...),
		LocationCode:    o.LocationCode,
		Contract:        contract,
		Pricing:         o.Pricing,
		ExistingVolumes: append([]string(nil), o.ExistingVolumes...),
		Volumes:         volumes,
		IsSpot:          o.IsSpot,
	}

	if o.StartupScriptID != "" {
		req.StartupScriptID = stringPtr(o.StartupScriptID)
	}
	if o.Coupon != "" {
		req.Coupon = stringPtr(o.Coupon)
	}
	if o.OSVolumeName != "" || o.OSVolumeSize > 0 || o.OSVolumeOnSpotDiscontinue != "" {
		if o.OSVolumeSize <= 0 {
			return verda.CreateInstanceRequest{}, fmt.Errorf("--os-volume-size must be positive when OS volume options are provided")
		}
		name := o.OSVolumeName
		if name == "" {
			name = fmt.Sprintf("%s-os", o.Hostname)
		}
		req.OSVolume = &verda.OSVolumeCreateRequest{
			Name:              name,
			Size:              o.OSVolumeSize,
			OnSpotDiscontinue: o.OSVolumeOnSpotDiscontinue,
		}
	}

	if err := req.Validate(); err != nil {
		return verda.CreateInstanceRequest{}, err
	}

	return req, nil
}

func (o *createOptions) descriptionValue() string {
	if strings.TrimSpace(o.Description) != "" {
		return o.Description
	}
	return o.Hostname
}

func normalizeContract(value string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(value))
	switch normalized {
	case "":
		return "", nil
	case "pay_as_go", "pay-as-go", "pay_as_you_go", "pay-as-you-go", "payg", "pay as go", "pay as you go":
		return "PAY_AS_YOU_GO", nil
	case "spot":
		return "SPOT", nil
	case "long_term", "long-term", "long term":
		return "LONG_TERM", nil
	case "1 month", "3 months", "6 months", "1 year", "2 years", "1_month", "3_months", "6_months", "1_year", "2_years":
		return "", fmt.Errorf("the current POST /v1/instances public API does not accept long-term duration values on instance creation; use --contract long_term only if your backend supports it")
	default:
		return "", fmt.Errorf("invalid --contract %q", value)
	}
}

func validateKind(kind string, instanceType string) error {
	if kind == "" {
		return nil
	}

	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "cpu":
		if !strings.HasPrefix(strings.ToUpper(instanceType), "CPU.") {
			return fmt.Errorf("--kind cpu does not match --instance-type %q", instanceType)
		}
	case "gpu":
		if strings.HasPrefix(strings.ToUpper(instanceType), "CPU.") {
			return fmt.Errorf("--kind gpu does not match --instance-type %q", instanceType)
		}
	default:
		return fmt.Errorf("invalid --kind %q: expected cpu or gpu", kind)
	}

	return nil
}

func parseVolumeSpecs(specs []string, isSpot bool) ([]verda.VolumeCreateRequest, error) {
	volumes := make([]verda.VolumeCreateRequest, 0, len(specs))
	for _, spec := range specs {
		volume, err := parseVolumeSpec(spec, isSpot)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, volume)
	}
	return volumes, nil
}

func parseVolumeSpec(spec string, isSpot bool) (verda.VolumeCreateRequest, error) {
	parts := strings.Split(spec, ":")
	if len(parts) < 3 || len(parts) > 5 {
		return verda.VolumeCreateRequest{}, fmt.Errorf("invalid --volume %q: expected name:size:type[:location[:on-spot-discontinue]]", spec)
	}

	size, err := strconv.Atoi(parts[1])
	if err != nil || size <= 0 {
		return verda.VolumeCreateRequest{}, fmt.Errorf("invalid --volume %q: size must be a positive integer", spec)
	}

	volume := verda.VolumeCreateRequest{
		Name: parts[0],
		Size: size,
		Type: parts[2],
	}
	if len(parts) >= 4 && parts[3] != "" {
		volume.LocationCode = parts[3]
	}
	if len(parts) == 5 && parts[4] != "" {
		if !isSpot {
			return verda.VolumeCreateRequest{}, fmt.Errorf("invalid --volume %q: on-spot-discontinue requires --is-spot", spec)
		}
		if _, ok := validSpotPolicies[parts[4]]; !ok {
			return verda.VolumeCreateRequest{}, fmt.Errorf("invalid --volume %q: unknown on-spot-discontinue policy %q", spec, parts[4])
		}
		volume.OnSpotDiscontinue = parts[4]
	}

	if err := volume.Validate(); err != nil {
		return verda.VolumeCreateRequest{}, fmt.Errorf("invalid --volume %q: %w", spec, err)
	}

	return volume, nil
}

func appendStorageVolume(volumes []verda.VolumeCreateRequest, o *createOptions) ([]verda.VolumeCreateRequest, error) {
	storageType := o.StorageType
	if storageType == "" {
		storageType = verda.VolumeTypeNVMe
	}

	if _, ok := validSpotPolicies[o.StorageOnSpotDiscontinue]; !ok {
		return nil, fmt.Errorf("invalid --storage-on-spot-discontinue %q", o.StorageOnSpotDiscontinue)
	}
	if o.StorageOnSpotDiscontinue != "" && !o.IsSpot {
		return nil, fmt.Errorf("--storage-on-spot-discontinue requires --is-spot")
	}

	if o.StorageSize == 0 && o.StorageName == "" && o.StorageOnSpotDiscontinue == "" {
		return volumes, nil
	}
	if o.StorageSize <= 0 {
		return nil, fmt.Errorf("--storage-size must be positive when storage options are provided")
	}

	name := o.StorageName
	if name == "" {
		name = fmt.Sprintf("%s-storage", o.Hostname)
	}

	volume := verda.VolumeCreateRequest{
		Name:              name,
		Size:              o.StorageSize,
		Type:              storageType,
		LocationCode:      o.LocationCode,
		OnSpotDiscontinue: o.StorageOnSpotDiscontinue,
	}
	if err := volume.Validate(); err != nil {
		return nil, fmt.Errorf("invalid --storage options: %w", err)
	}

	return append(volumes, volume), nil
}

func stringPtr(v string) *string {
	return &v
}
