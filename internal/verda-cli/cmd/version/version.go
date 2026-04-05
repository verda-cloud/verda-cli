package version

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/verda-cloud/verdagostack/pkg/version"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// versionOutput extends version.Info with dependency versions for structured output.
type versionOutput struct {
	version.Info
	SDKVersion   string `json:"sdkVersion"`
	StackVersion string `json:"stackVersion"`
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

// NewCmdVersion creates the version cobra command.
func NewCmdVersion(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var verify bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Long:  cmdutil.LongDesc("Print the build and version information for verda."),
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Get()

			if verify {
				return runVerify(ioStreams.Out, ioStreams.ErrOut, f.OutputFormat(), f.HTTPClient(), info.GitVersion, runtime.GOOS, runtime.GOARCH)
			}

			sdkVer := depVersion("github.com/verda-cloud/verdacloud-sdk-go")
			stackVer := depVersion("github.com/verda-cloud/verdagostack")

			out := versionOutput{
				Info:         info,
				SDKVersion:   sdkVer,
				StackVersion: stackVer,
			}
			if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), out); wrote {
				return err
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "  Version:      %s\n  Platform:     %s\n  SDK:          %s\n  Verdagostack: %s\n",
				info.GitVersion, info.Platform, sdkVer, stackVer)
			return nil
		},
	}

	cmd.Flags().BoolVar(&verify, "verify", false, "Verify the binary checksum against the GitHub release")

	return cmd
}
