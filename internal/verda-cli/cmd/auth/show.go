package auth

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github/verda-cloud/verda-cli/internal/verda-cli/options"
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
			// Resolve credentials best-effort so show always works.
			opts.Complete()

			auth := opts.AuthOptions
			_, _ = fmt.Fprintf(ioStreams.Out, "profile:              %s\n", auth.Profile)
			_, _ = fmt.Fprintf(ioStreams.Out, "credentials_file:     %s\n", auth.CredentialsFile)
			_, _ = fmt.Fprintf(ioStreams.Out, "base_url:             %s\n", opts.Server)
			_, _ = fmt.Fprintf(ioStreams.Out, "client_id_loaded:     %t\n", auth.ClientID != "")
			_, _ = fmt.Fprintf(ioStreams.Out, "client_secret_loaded: %t\n", auth.ClientSecret != "")

			// If credentials failed to resolve, show a helpful hint.
			if auth.ClientID == "" || auth.ClientSecret == "" {
				profiles, _ := options.ListProfiles(auth.CredentialsFile)
				_, _ = fmt.Fprintln(ioStreams.ErrOut)
				if len(profiles) > 0 {
					_, _ = fmt.Fprintf(ioStreams.ErrOut, "Available profiles:   %s\n", strings.Join(profiles, ", "))
					_, _ = fmt.Fprintf(ioStreams.ErrOut, "Hint: run 'verda auth use' to switch profile, or 'verda auth login' to add one.\n")
				} else {
					_, _ = fmt.Fprintf(ioStreams.ErrOut, "No profiles found. Run 'verda auth login' to set up credentials.\n")
				}
			}
			return nil
		},
	}
}
