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

package cmd

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/verda-cloud/verdagostack/pkg/log"
	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/version"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/auth"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/availability"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/completion"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/cost"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/doctor"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/images"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/instancetypes"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/locations"
	mcpcmd "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/mcp"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/registry"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/s3"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/settings"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/skills"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/ssh"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/sshkey"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/startupscript"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/status"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/template"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/update"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/vm"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/volume"
	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// NewRootCommand creates the root `verda` cobra command with all subcommands
// organized into logical groups. It also returns the resolved Options so
// callers (e.g. main) can annotate errors with profile context.
func NewRootCommand(ioStreams cmdutil.IOStreams) (*cobra.Command, *clioptions.Options) {
	opts := clioptions.NewOptions()
	var showVersion bool

	cmd := &cobra.Command{
		Use:   "verda",
		Short: "Command-line interface for Verda Cloud",
		Long: cmdutil.LongDesc(`
			Command-line interface for Verda Cloud.`),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), versionOutput())
				return ErrVersionRequested
			}
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Agent mode always implies JSON output and no TUI. Apply
			// this before the credential-skip check so commands that
			// bypass Complete() (skills, mcp serve, auth show/use) still
			// get the right output mode and suppress spinners.
			if opts.Agent {
				opts.Output = "json"
			}

			// Skip heavy credential resolution for commands that don't need it:
			// - mcp serve: defers auth to the first tool call
			// - auth show: diagnostic command that should work even without valid credentials
			// - auth use: switches profiles, doesn't need current credentials
			if skipCredentialResolution(cmd) {
				log.Init(opts.Log)
				return nil
			}
			opts.Complete()
			if err := opts.Validate(); err != nil {
				return err
			}
			log.Init(opts.Log)
			// Apply saved theme (best effort).
			if theme := viper.GetString("settings.theme"); theme != "" {
				bubbletea.SetThemeByName(theme)
			}
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, _ []string) {
			// Show version-update hint (best-effort, never fails the command).
			if opts.Agent || opts.Output != "table" {
				return
			}
			switch cmd.Name() {
			case "update", "doctor", "completion":
				return
			}
			latest, current, err := cmdutil.CheckVersion(cmd.Context())
			if err != nil {
				return
			}
			cmdutil.PrintVersionHint(ioStreams.ErrOut, latest, current)
		},
	}

	// --version / -v flag: print rich version info.
	cmd.PersistentFlags().BoolVarP(&showVersion, "version", "v", false, "Print version information")

	opts.AddFlags(cmd.PersistentFlags())
	_ = viper.BindPFlags(cmd.PersistentFlags())

	// Register completion values for the global --output flag.
	_ = cmd.RegisterFlagCompletionFunc("output", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"json", "yaml", "table"}, cobra.ShellCompDirectiveNoFileComp
	})

	cobra.OnInitialize(func() {
		initConfig(viper.GetString(clioptions.FlagConfig))
	})

	f := cmdutil.NewFactory(opts)

	resourceCmds := []*cobra.Command{
		availability.NewCmdAvailability(f, ioStreams),
		images.NewCmdImages(f, ioStreams),
		instancetypes.NewCmdInstanceTypes(f, ioStreams),
		locations.NewCmdLocations(f, ioStreams),
		sshkey.NewCmdSSHKey(f, ioStreams),
		startupscript.NewCmdStartupScript(f, ioStreams),
		template.NewCmdTemplate(f, ioStreams),
		volume.NewCmdVolume(f, ioStreams),
	}
	if s3Enabled() {
		resourceCmds = append(resourceCmds, s3.NewCmdS3(f, ioStreams))
	}
	if registryEnabled() {
		resourceCmds = append(resourceCmds, registry.NewCmdRegistry(f, ioStreams))
	}

	groups := cmdutil.CommandGroups{
		{
			Message: "Auth Commands:",
			Commands: []*cobra.Command{
				auth.NewCmdAuth(f, ioStreams),
			},
		},
		{
			Message: "VM Commands:",
			Commands: []*cobra.Command{
				vm.NewCmdVM(f, ioStreams),
				ssh.NewCmdSSH(f, ioStreams),
			},
		},
		{
			Message:  "Resource Commands:",
			Commands: resourceCmds,
		},
		{
			Message: "Info Commands:",
			Commands: []*cobra.Command{
				status.NewCmdStatus(f, ioStreams),
				cost.NewCmdCost(f, ioStreams),
			},
		},
		{
			Message: "AI Agent Commands:",
			Commands: []*cobra.Command{
				mcpcmd.NewCmdMCP(f, ioStreams),
				skills.NewCmdSkills(f, ioStreams),
			},
		},
		{
			Message: "Other Commands:",
			Commands: []*cobra.Command{
				completion.NewCmdCompletion(ioStreams),
				doctor.NewCmdDoctor(f, ioStreams),
				settings.NewCmdSettings(f, ioStreams),
				update.NewCmdUpdate(f, ioStreams),
			},
		},
	}

	groups.Add(cmd)
	cmdutil.SetUsageTemplate(cmd, groups)

	return cmd, opts
}

// s3Enabled gates the pre-release S3 object-storage commands. The whole
// command tree is omitted from registration unless VERDA_S3_ENABLED is "1"
// or "true". When the feature ships GA, delete this function, drop the
// gate in NewRootCommand, and remove `Hidden: true` from cmd/s3/s3.go.
func s3Enabled() bool {
	v := os.Getenv("VERDA_S3_ENABLED")
	return v == "1" || v == "true"
}

// registryEnabled gates the pre-release Verda Container Registry commands.
// The whole command tree is omitted from registration unless
// VERDA_REGISTRY_ENABLED is "1" or "true". When the feature ships GA,
// delete this function, drop the gate in NewRootCommand, and remove
// `Hidden: true` from cmd/registry/registry.go.
func registryEnabled() bool {
	v := os.Getenv("VERDA_REGISTRY_ENABLED")
	return v == "1" || v == "true"
}

// skipCredentialResolution returns true for commands that should work
// without valid credentials (diagnostics, profile switching, etc.).
func skipCredentialResolution(cmd *cobra.Command) bool {
	parent := cmd.Parent()
	if parent == nil {
		return false
	}
	pName := parent.Name()
	switch {
	case cmd.Name() == "serve" && pName == "mcp":
		return true
	case cmd.Name() == "show" && pName == "auth":
		return true
	case cmd.Name() == "use" && pName == "auth":
		return true
	case pName == "skills":
		return true
	case pName == "s3":
		return true
	case pName == "registry":
		return true
	case cmd.Name() == "doctor" && pName == "verda":
		return true
	}
	return false
}

// ErrVersionRequested is returned by PersistentPreRunE when --version is set.
// Callers should check for this error and exit 0 instead of printing it.
var ErrVersionRequested = errors.New("version requested")

// versionOutput returns the formatted version string.
func versionOutput() string {
	info := version.Get()
	sdkVer := depVersion("github.com/verda-cloud/verdacloud-sdk-go")
	stackVer := depVersion("github.com/verda-cloud/verdagostack")
	return fmt.Sprintf("  Version:      %s\n  Platform:     %s\n  SDK:          %s\n  Verdagostack: %s\n",
		info.GitVersion, info.Platform, sdkVer, stackVer)
}

func depVersion(modulePath string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range bi.Deps {
		if dep.Path == modulePath {
			if dep.Replace != nil {
				return dep.Replace.Version + " (replaced)"
			}
			return dep.Version
		}
	}
	return "unknown"
}
