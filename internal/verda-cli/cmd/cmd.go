package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/verda-cloud/verdagostack/pkg/log"
	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"

	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/auth"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/availability"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/completion"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/cost"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/images"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/instancetypes"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/locations"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/settings"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/ssh"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/sshkey"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/startupscript"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/update"
	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/version"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/vm"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/volume"
	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
)

// NewRootCommand creates the root `verda` cobra command with all subcommands
// organized into logical groups.
func NewRootCommand(ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := clioptions.NewOptions()

	cmd := &cobra.Command{
		Use:   "verda",
		Short: "Command-line interface for Verda Cloud",
		Long: cmdutil.LongDesc(`
			Command-line interface for Verda Cloud.`),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
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

	opts.AddFlags(cmd.PersistentFlags())
	_ = viper.BindPFlags(cmd.PersistentFlags())

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
				cost.NewCmdCost(f, ioStreams),
			},
		},
		{
			Message: "Other Commands:",
			Commands: []*cobra.Command{
				completion.NewCmdCompletion(ioStreams),
				settings.NewCmdSettings(f, ioStreams),
				update.NewCmdUpdate(f, ioStreams),
				version.NewCmdVersion(f, ioStreams),
			},
		},
	}

	groups.Add(cmd)
	cmdutil.SetUsageTemplate(cmd, groups)

	return cmd
}
