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

type infernoBuildOptions struct {
	Tag           string
	BaseImage     string
	PythonVersion string
	SocketDir     string
}

func newCmdInfernoBuild(_ cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &infernoBuildOptions{}

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build Inferno container images for Verda Cloud",
		Long: cmdutil.LongDesc(`
			Build split container images (server + worker) for deploying on Verda
			Cloud. Runs 'inferno build --target verda' to produce images ready for
			deployment.

			Requires the 'inferno' CLI to be installed and available in PATH.
		`),
		Example: cmdutil.Examples(`
			# Build with default settings
			verda inferno build

			# Build with a custom tag
			verda inferno build -t my-model:v1.0

			# Build with a custom base image
			verda inferno build --base-image nvidia/cuda:12.8.0-cudnn-runtime-ubuntu22.04
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInfernoBuild(cmd.Context(), ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.Tag, "tag", "t", "", "Custom image tag")
	flags.StringVar(&opts.BaseImage, "base-image", "", "Custom base Docker image")
	flags.StringVar(&opts.PythonVersion, "python-version", "", "Python version (e.g. 3.12)")
	flags.StringVar(&opts.SocketDir, "socket-dir", "", "Custom socket directory path in containers")

	return cmd
}

func runInfernoBuild(ctx context.Context, ioStreams cmdutil.IOStreams, opts *infernoBuildOptions) error {
	infernoPath, err := exec.LookPath("inferno")
	if err != nil {
		return errors.New("'inferno' CLI not found in PATH\n\n" +
			"Install it from the Verda Inferno repository:\n" +
			"  make install")
	}

	cmdArgs := []string{"build", "--target", "verda"}
	if opts.Tag != "" {
		cmdArgs = append(cmdArgs, "-t", opts.Tag)
	}
	if opts.BaseImage != "" {
		cmdArgs = append(cmdArgs, "--base-image", opts.BaseImage)
	}
	if opts.PythonVersion != "" {
		cmdArgs = append(cmdArgs, "--python-version", opts.PythonVersion)
	}
	if opts.SocketDir != "" {
		cmdArgs = append(cmdArgs, "--socket-dir", opts.SocketDir)
	}

	c := exec.CommandContext(ctx, infernoPath, cmdArgs...)
	c.Stdin = os.Stdin
	c.Stdout = ioStreams.Out
	c.Stderr = ioStreams.ErrOut
	return c.Run()
}
