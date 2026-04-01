package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdShow creates the auth show command.
func NewCmdShow(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the resolved auth profile",
		Long: cmdutil.LongDesc(`
			Show the active auth profile and shared credentials file path.
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := f.Options()
			auth := opts.AuthOptions
			_, _ = fmt.Fprintf(ioStreams.Out, "profile:              %s\n", auth.Profile)
			_, _ = fmt.Fprintf(ioStreams.Out, "credentials_file:     %s\n", auth.CredentialsFile)
			_, _ = fmt.Fprintf(ioStreams.Out, "base_url:             %s\n", opts.Server)
			_, _ = fmt.Fprintf(ioStreams.Out, "client_id_loaded:     %t\n", auth.ClientID != "")
			_, _ = fmt.Fprintf(ioStreams.Out, "client_secret_loaded: %t\n", auth.ClientSecret != "")
			return nil
		},
	}
}
