package completion

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdCompletion creates the completion command for generating shell scripts.
func NewCmdCompletion(ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: cmdutil.LongDesc(`
			Generate shell completion scripts for verda.

			Bash:
			  source <(verda completion bash)

			  # To load completions for each session, execute once:
			  # Linux:
			  verda completion bash > /etc/bash_completion.d/verda
			  # macOS:
			  verda completion bash > $(brew --prefix)/etc/bash_completion.d/verda

			Zsh:
			  # If shell completion is not already enabled in your environment,
			  # you will need to enable it. You can execute the following once:
			  echo "autoload -U compinit; compinit" >> ~/.zshrc

			  # To load completions for each session, execute once:
			  verda completion zsh > "${fpath[1]}/_verda"

			  # You will need to start a new shell for this setup to take effect.

			Fish:
			  verda completion fish | source

			  # To load completions for each session, execute once:
			  verda completion fish > ~/.config/fish/completions/verda.fish

			PowerShell:
			  verda completion powershell | Out-String | Invoke-Expression

			  # To load completions for every new session, add the output to your profile:
			  verda completion powershell >> $PROFILE
		`),
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompletion(cmd, ioStreams, args[0])
		},
	}
	return cmd
}

func runCompletion(cmd *cobra.Command, ioStreams cmdutil.IOStreams, shell string) error {
	switch shell {
	case "bash":
		return cmd.Root().GenBashCompletionV2(ioStreams.Out, true)
	case "zsh":
		return cmd.Root().GenZshCompletion(ioStreams.Out)
	case "fish":
		return cmd.Root().GenFishCompletion(ioStreams.Out, true)
	case "powershell":
		return cmd.Root().GenPowerShellCompletionWithDesc(ioStreams.Out)
	}
	return nil
}
