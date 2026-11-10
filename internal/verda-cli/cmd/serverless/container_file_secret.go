// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package serverless

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newCmdContainerFileSecret(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "file-secret",
		Short: "Manage serverless file secrets",
		Long: cmdutil.LongDesc(`
			Create, list, and delete file secrets (filesets) used by serverless
			deployments. File secrets bundle one or more files that are mounted
			into containers at runtime.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		newCmdFileSecretList(f, ioStreams),
		newCmdFileSecretCreate(f, ioStreams),
		newCmdFileSecretDelete(f, ioStreams),
	)
	return cmd
}

// --- list ---

func newCmdFileSecretList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List file secrets",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			secrets, err := cmdutil.WithSpinner(ctx, f.Status(), "Fetching file secrets...", func(ctx context.Context) ([]verda.FileSecret, error) {
				return client.ContainerDeployments.GetFileSecrets(ctx)
			})
			if err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), secrets); wrote {
				return werr
			}
			if len(secrets) == 0 {
				_, _ = fmt.Fprintln(ioStreams.Out, "No file secrets found.")
				return nil
			}
			w := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tTYPE\tFILES\tCREATED")
			for _, s := range secrets {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					s.Name, s.SecretType,
					strings.Join(s.FileNames, ", "),
					s.CreatedAt.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

// --- create ---

func newCmdFileSecretCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var name string
	var files []string // "name=path" pairs

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a file secret",
		Long: cmdutil.LongDesc(`
			Create a file secret from one or more local files. Each --file flag
			takes a name=path pair: the name is used inside the container mount,
			and the path points to the local file whose contents will be uploaded.
		`),
		Example: cmdutil.Examples(`
			# Create a file secret with one file
			verda container file-secret create --name my-certs --file ca.pem=./certs/ca.pem

			# Multiple files
			verda container file-secret create --name tls-bundle \
			  --file cert.pem=./cert.pem --file key.pem=./key.pem
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.AgentMode() {
				var missing []string
				if name == "" {
					missing = append(missing, "--name")
				}
				if len(files) == 0 {
					missing = append(missing, "--file")
				}
				if len(missing) > 0 {
					return cmdutil.NewMissingFlagsError(missing)
				}
			}

			if name == "" {
				v, err := f.Prompter().TextInput(cmd.Context(), "File secret name:")
				if err != nil {
					if isPromptCancel(err) {
						return nil
					}
					return err
				}
				name = v
			}
			if len(files) == 0 {
				prompted, err := promptFileEntries(cmd.Context(), f.Prompter())
				if err != nil {
					return err
				}
				if len(prompted) == 0 {
					return nil
				}
				files = prompted
			}

			fsFiles, err := parseFileFlags(files)
			if err != nil {
				return err
			}

			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			req := &verda.CreateFileSecretRequest{Name: name, Files: fsFiles}
			cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

			if err := cmdutil.RunWithSpinner(ctx, f.Status(), "Creating file secret...", func(ctx context.Context) error {
				return client.ContainerDeployments.CreateFileSecret(ctx, req)
			}); err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), map[string]any{
				"name": name, "action": "created", "files": len(fsFiles),
			}); wrote {
				return werr
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "Created file secret %q (%d files)\n", name, len(fsFiles))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "File secret name")
	cmd.Flags().StringArrayVar(&files, "file", nil, "File as name=path (repeat for multiple)")
	return cmd
}

func promptFileEntries(ctx context.Context, p tui.Prompter) ([]string, error) {
	var files []string
	for {
		path, err := p.TextInput(ctx, fmt.Sprintf("File path (have %d, empty to finish):", len(files)))
		if err != nil {
			if isPromptCancel(err) {
				return nil, nil
			}
			return nil, err
		}
		path = strings.TrimSpace(path)
		if path == "" {
			break
		}
		fname, err := p.TextInput(ctx, "Name inside container:")
		if err != nil {
			if isPromptCancel(err) {
				return nil, nil
			}
			return nil, err
		}
		files = append(files, strings.TrimSpace(fname)+"="+path)
	}
	if len(files) == 0 {
		return nil, errors.New("at least one file is required")
	}
	return files, nil
}

// parseFileFlags reads each "name=path" entry, reads the file, and base64-encodes it.
func parseFileFlags(entries []string) ([]verda.FileSecretFile, error) {
	out := make([]verda.FileSecretFile, 0, len(entries))
	for _, entry := range entries {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid --file %q: expected name=path", entry)
		}
		data, err := os.ReadFile(parts[1])
		if err != nil {
			return nil, fmt.Errorf("reading file %q: %w", parts[1], err)
		}
		out = append(out, verda.FileSecretFile{
			Name:          parts[0],
			Base64Content: base64.StdEncoding.EncodeToString(data),
		})
	}
	return out, nil
}

// --- delete ---

func newCmdFileSecretDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var force, yes bool

	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm", "del"},
		Short:   "Delete a file secret",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if f.AgentMode() && !yes {
				return cmdutil.NewConfirmationRequiredError("delete file secret")
			}
			if !yes {
				detail := ""
				if force {
					detail = "Force-deleting will remove the file secret even if it is in use."
				}
				ok, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
					"Delete file secret "+name, detail, "Delete file secret "+name+"?")
				if err != nil {
					return err
				}
				if !ok {
					return nil
				}
			}

			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			if err := cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting file secret...", func(ctx context.Context) error {
				return client.ContainerDeployments.DeleteFileSecret(ctx, name, force)
			}); err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), map[string]string{
				"name": name, "action": "deleted",
			}); wrote {
				return werr
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "Deleted file secret %q\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force delete even if the file secret is in use")
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, "Skip confirmation (required in agent mode)")
	return cmd
}
