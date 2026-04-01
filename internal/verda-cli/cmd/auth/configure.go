package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/ini.v1"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type configureOptions struct {
	Profile         string
	CredentialsFile string
	ClientID        string
	ClientSecret    string
	BearerToken     string
}

// NewCmdConfigure creates the auth configure command.
func NewCmdConfigure(_ cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &configureOptions{
		Profile: "default",
	}

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Write shared credentials for a profile",
		Long: cmdutil.LongDesc(`
			Write credentials to ~/.verda/credentials using an AWS-style INI format.
		`),
		Example: cmdutil.Examples(`
			verda auth configure \
			  --profile default \
			  --client-id your-client-id \
			  --client-secret your-client-secret

			verda auth configure \
			  --profile staging \
			  --client-id your-staging-client-id \
			  --client-secret your-staging-client-secret
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.ClientID) == "" {
				return cmdutil.UsageErrorf(cmd, "--client-id is required")
			}
			if strings.TrimSpace(opts.ClientSecret) == "" {
				return cmdutil.UsageErrorf(cmd, "--client-secret is required")
			}

			path, err := resolveCredentialsFile(opts.CredentialsFile)
			if err != nil {
				return err
			}

			cfg := ini.Empty()
			if _, err := os.Stat(path); err == nil {
				cfg, err = ini.Load(path)
				if err != nil {
					return err
				}
			}

			section, err := cfg.GetSection(opts.Profile)
			if err != nil {
				section, err = cfg.NewSection(opts.Profile)
				if err != nil {
					return err
				}
			}

			section.Key("client_id").SetValue(opts.ClientID)
			section.Key("client_secret").SetValue(opts.ClientSecret)
			if opts.BearerToken != "" {
				section.Key("token").SetValue(opts.BearerToken)
			} else if section.HasKey("token") {
				section.DeleteKey("token")
			}

			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			if err := cfg.SaveTo(path); err != nil {
				return err
			}
			if err := os.Chmod(path, 0o600); err != nil {
				return err
			}

			fmt.Fprintf(ioStreams.Out, "Saved profile %q to %s\n", opts.Profile, path)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile name")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared credentials file")
	flags.StringVar(&opts.ClientID, "client-id", "", "Verda API client ID")
	flags.StringVar(&opts.ClientSecret, "client-secret", "", "Verda API client secret")
	flags.StringVar(&opts.BearerToken, "token", "", "Optional bearer token to store with the profile")

	return cmd
}
