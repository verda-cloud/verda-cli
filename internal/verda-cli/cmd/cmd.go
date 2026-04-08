package cmd

import (
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/verda-cloud/verdagostack/pkg/log"
	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/version"

	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/auth"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/availability"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/completion"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/cost"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/images"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/instancetypes"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/locations"
	mcpcmd "github/verda-cloud/verda-cli/internal/verda-cli/cmd/mcp"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/settings"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/ssh"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/sshkey"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/startupscript"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/status"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/update"
	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/vm"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/volume"
	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
)

// NewRootCommand creates the root `verda` cobra command with all subcommands
// organized into logical groups. It also returns the resolved Options so
// callers (e.g. main) can annotate errors with profile context.
func NewRootCommand(ioStreams cmdutil.IOStreams) (*cobra.Command, *clioptions.Options) {
	opts := clioptions.NewOptions()
	var showVersion bool

	cmd := &cobra.Command{
		Use:   "verda",
		Short: "Command-line interface for Verda Cloud",
		Long: cmdutil.LongDesc(`
			Command-line interface for Verda Cloud.`),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), versionOutput())
				return ErrVersionRequested
			}
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip heavy credential resolution for commands that don't need it:
			// - mcp serve: defers auth to the first tool call
			// - auth show: diagnostic command that should work even without valid credentials
			// - auth use: switches profiles, doesn't need current credentials
			if skipCredentialResolution(cmd) {
				log.Init(opts.Log)
				return nil
			}
			opts.Complete()
			if err := opts.Validate(); err != nil {
				return err
			}
			log.Init(opts.Log)
			// Apply saved theme (best effort).
			if theme := viper.GetString("settings.theme"); theme != "" {
				bubbletea.SetThemeByName(theme)
			}
			return nil
		},
	}

	// --version / -v flag: print rich version info.
	cmd.PersistentFlags().BoolVarP(&showVersion, "version", "v", false, "Print version information")

	opts.AddFlags(cmd.PersistentFlags())
	_ = viper.BindPFlags(cmd.PersistentFlags())

	// Register completion values for the global --output flag.
	_ = cmd.RegisterFlagCompletionFunc("output", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"json", "yaml", "table"}, cobra.ShellCompDirectiveNoFileComp
	})

	cobra.OnInitialize(func() {
		initConfig(viper.GetString(clioptions.FlagConfig))
	})

	f := cmdutil.NewFactory(opts)

	groups := cmdutil.CommandGroups{
		{
			Message: "Auth Commands:",
			Commands: []*cobra.Command{
				auth.NewCmdAuth(f, ioStreams),
			},
		},
		{
			Message: "VM Commands:",
			Commands: []*cobra.Command{
				vm.NewCmdVM(f, ioStreams),
				ssh.NewCmdSSH(f, ioStreams),
			},
		},
		{
			Message: "Resource Commands:",
			Commands: []*cobra.Command{
				availability.NewCmdAvailability(f, ioStreams),
				images.NewCmdImages(f, ioStreams),
				instancetypes.NewCmdInstanceTypes(f, ioStreams),
				locations.NewCmdLocations(f, ioStreams),
				sshkey.NewCmdSSHKey(f, ioStreams),
				startupscript.NewCmdStartupScript(f, ioStreams),
				volume.NewCmdVolume(f, ioStreams),
			},
		},
		{
			Message: "Info Commands:",
			Commands: []*cobra.Command{
				status.NewCmdStatus(f, ioStreams),
				cost.NewCmdCost(f, ioStreams),
			},
		},
		{
			Message: "AI Agent Commands:",
			Commands: []*cobra.Command{
				mcpcmd.NewCmdMCP(f, ioStreams),
			},
		},
		{
			Message: "Other Commands:",
			Commands: []*cobra.Command{
				completion.NewCmdCompletion(ioStreams),
				settings.NewCmdSettings(f, ioStreams),
				update.NewCmdUpdate(f, ioStreams),
			},
		},
	}

	groups.Add(cmd)
	cmdutil.SetUsageTemplate(cmd, groups)

	return cmd, opts
}

// skipCredentialResolution returns true for commands that should work
// without valid credentials (diagnostics, profile switching, etc.).
func skipCredentialResolution(cmd *cobra.Command) bool {
	parent := cmd.Parent()
	if parent == nil {
		return false
	}
	pName := parent.Name()
	switch {
	case cmd.Name() == "serve" && pName == "mcp":
		return true
	case cmd.Name() == "show" && pName == "auth":
		return true
	case cmd.Name() == "use" && pName == "auth":
		return true
	}
	return false
}

// ErrVersionRequested is returned by PersistentPreRunE when --version is set.
// Callers should check for this error and exit 0 instead of printing it.
var ErrVersionRequested = errors.New("version requested")

// versionOutput returns the formatted version string.
func versionOutput() string {
	info := version.Get()
	sdkVer := depVersion("github.com/verda-cloud/verdacloud-sdk-go")
	stackVer := depVersion("github.com/verda-cloud/verdagostack")
	return fmt.Sprintf("  Version:      %s\n  Platform:     %s\n  SDK:          %s\n  Verdagostack: %s\n",
		info.GitVersion, info.Platform, sdkVer, stackVer)
}

func depVersion(modulePath string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range bi.Deps {
		if dep.Path == modulePath {
			if dep.Replace != nil {
				return dep.Replace.Version + " (replaced)"
			}
			return dep.Version
		}
	}
	return "unknown"
}
