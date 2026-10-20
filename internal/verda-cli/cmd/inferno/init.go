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

package inferno

import (
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type infernoInitOptions struct {
	Name               string
	Force              bool
	LegacyRequirements bool
}

func newCmdInfernoInit(_ cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &infernoInitOptions{}

	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Initialize a new Inferno ML project",
		Long: cmdutil.LongDesc(`
			Initialize a new Verda Inferno ML inference project. Creates the
			project directory with config.yaml, main.py, pyproject.toml, and
			.dockerignore.

			Requires the 'inferno' CLI to be installed and available in PATH.
		`),
		Example: cmdutil.Examples(`
			# Create a new project
			verda inferno init my-model

			# Create with a custom project name
			verda inferno init my-model --name custom-name

			# Overwrite existing files
			verda inferno init my-model --force
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfernoInit(cmd.Context(), ioStreams, opts, args)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Set explicit project name")
	flags.BoolVar(&opts.Force, "force", false, "Overwrite existing files")
	flags.BoolVar(&opts.LegacyRequirements, "legacy-requirements", false, "Use requirements.txt instead of pyproject.toml")

	return cmd
}

func runInfernoInit(ctx context.Context, ioStreams cmdutil.IOStreams, opts *infernoInitOptions, args []string) error {
	infernoPath, err := exec.LookPath("inferno")
	if err != nil {
		return errors.New("'inferno' CLI not found in PATH\n\n" +
			"Install it from the Verda Inferno repository:\n" +
			"  make install")
	}

	cmdArgs := []string{"init"}
	if len(args) > 0 {
		cmdArgs = append(cmdArgs, args[0])
	}
	if opts.Name != "" {
		cmdArgs = append(cmdArgs, "--name", opts.Name)
	}
	if opts.Force {
		cmdArgs = append(cmdArgs, "--force")
	}
	if opts.LegacyRequirements {
		cmdArgs = append(cmdArgs, "--legacy-requirements")
	}

	c := exec.CommandContext(ctx, infernoPath, cmdArgs...)
	c.Stdin = os.Stdin
	c.Stdout = ioStreams.Out
	c.Stderr = ioStreams.ErrOut
	return c.Run()
}
