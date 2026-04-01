package sshkey

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type addOptions struct {
	Name      string
	PublicKey string
}

// NewCmdAdd creates the ssh-key add cobra command.
func NewCmdAdd(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &addOptions{}

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add an SSH key",
		Long: cmdutil.LongDesc(`
			Add a new SSH key to your account. In interactive mode you will be
			prompted for the key name and public key. Use --name and --public-key
			flags for non-interactive use.
		`),
		Example: cmdutil.Examples(`
			# Interactive
			verda ssh-key add

			# Non-interactive
			verda ssh-key add --name my-key --public-key "ssh-ed25519 AAAA..."
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "SSH key name")
	flags.StringVar(&opts.PublicKey, "public-key", "", "SSH public key content")

	return cmd
}

func runAdd(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *addOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	name := opts.Name
	if name == "" {
		name, err = prompter.TextInput(ctx, "SSH key name")
		if err != nil {
			return nil //nolint:nilerr
		}
		if name == "" {
			return fmt.Errorf("name is required")
		}
	}

	publicKey := opts.PublicKey
	if publicKey == "" {
		publicKey, err = prompter.TextInput(ctx, "Public key (paste)")
		if err != nil {
			return nil //nolint:nilerr
		}
		if publicKey == "" {
			return fmt.Errorf("public key is required")
		}
	}

	createCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(createCtx, "Adding SSH key...")
	}
	key, err := client.SSHKeys.AddSSHKey(createCtx, &verda.CreateSSHKeyRequest{
		Name:      name,
		PublicKey: publicKey,
	})
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Added SSH key: %s (%s)\n", key.Name, key.ID)
	return nil
}
