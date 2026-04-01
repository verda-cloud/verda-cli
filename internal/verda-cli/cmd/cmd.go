package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/verda-cloud/verdagostack/pkg/log"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/auth"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/sshkey"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/startupscript"
	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/version"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/vm"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/volume"
	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
)

// NewRootCommand creates the root `verda` cobra command with all subcommands
// organised into logical groups.
func NewRootCommand(ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := clioptions.NewOptions()

	cmd := &cobra.Command{
		Use:   "verda",
		Short: "Command-line interface for Verda Cloud",
		Long: cmdutil.LongDesc(`
			Command-line interface for Verda Cloud.

			Configuration is resolved from multiple sources in order of precedence:
			  1. Command-line flags (highest priority)
			  2. Environment variables with VERDA_ prefix
			  3. Config file: ~/.verda/config.yaml or --config path
			  4. Built-in defaults`),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			opts.Complete()
			if err := opts.Validate(); err != nil {
				return err
			}
			log.Init(opts.Log)
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
			},
		},
		{
			Message: "Resource Commands:",
			Commands: []*cobra.Command{
				sshkey.NewCmdSSHKey(f, ioStreams),
				startupscript.NewCmdStartupScript(f, ioStreams),
				volume.NewCmdVolume(f, ioStreams),
			},
		},
		{
			Message: "Other Commands:",
			Commands: []*cobra.Command{
				version.NewCmdVersion(f, ioStreams),
			},
		},
	}

	groups.Add(cmd)
	cmdutil.SetUsageTemplate(cmd, groups)

	return cmd
}
