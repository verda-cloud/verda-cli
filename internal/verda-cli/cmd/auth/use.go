package auth

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// NewCmdUse creates the auth use command.
func NewCmdUse(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var credentialsFile string

	cmd := &cobra.Command{
		Use:   "use [PROFILE]",
		Short: "Switch the active auth profile",
		Long: cmdutil.LongDesc(`
			Set the default auth profile in ~/.verda/config.yaml.

			If no profile name is given, an interactive list of available
			profiles is shown.
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveCredentialsFile(credentialsFile)
			if err != nil {
				return err
			}

			var profile string
			if len(args) > 0 {
				profile = args[0]
			} else {
				// Interactive: list profiles and let user pick.
				profiles, err := options.ListProfiles(path)
				if err != nil {
					return err
				}
				if len(profiles) == 0 {
					return fmt.Errorf("no profiles found in %s — run 'verda auth login' first", path)
				}

				idx, err := f.Prompter().Select(cmd.Context(), "Select profile", profiles)
				if err != nil {
					return nil //nolint:nilerr // User pressed Esc/Ctrl+C.
				}
				profile = profiles[idx]
			}

			// Validate that the profile exists in the credentials file.
			if _, err := options.LoadSharedCredentialsForProfile(path, profile); err != nil {
				return err
			}

			configPath, err := defaultConfigFilePath()
			if err != nil {
				return err
			}
			if err := writeActiveProfile(configPath, profile); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(ioStreams.Out, "Active auth profile: %s\n", profile)
			return nil
		},
	}

	cmd.Flags().StringVar(&credentialsFile, "credentials-file", "", "Path to the shared credentials file")
	return cmd
}

func writeActiveProfile(path, profile string) error {
	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // controlled config path
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	authCfg, _ := cfg["auth"].(map[string]any)
	if authCfg == nil {
		authCfg = map[string]any{}
	}
	authCfg["profile"] = profile
	cfg["auth"] = authCfg

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return options.WriteSecureFile(path, data)
}
