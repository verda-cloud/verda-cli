package auth

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github/verda-cloud/verda-cli/internal/verda-cli/options"
)

// NewCmdUse creates the auth use command.
func NewCmdUse(_ cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var credentialsFile string

	cmd := &cobra.Command{
		Use:   "use PROFILE",
		Short: "Switch the active auth profile",
		Long: cmdutil.LongDesc(`
			Set the default auth profile in ~/.verda/config.yaml.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]
			path, err := resolveCredentialsFile(credentialsFile)
			if err != nil {
				return err
			}
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

func writeActiveProfile(path string, profile string) error {
	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // path is from our own config
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
