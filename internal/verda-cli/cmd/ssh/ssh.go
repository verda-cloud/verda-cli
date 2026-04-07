package ssh

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type sshOptions struct {
	User    string
	KeyFile string
}

// NewCmdSSH creates the ssh command for connecting to a running instance.
func NewCmdSSH(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &sshOptions{User: "root"}

	cmd := &cobra.Command{
		Use:   "ssh <instance-id-or-hostname> [-- extra-ssh-args...]",
		Short: "SSH into a running VM instance",
		Long: cmdutil.LongDesc(`
			Connect to a running VM instance via SSH.

			The instance can be specified by ID or hostname. The command
			resolves the instance's IP address from the API and opens an
			SSH connection.

			Any arguments after -- are passed directly to the ssh command.
		`),
		Example: cmdutil.Examples(`
			# SSH by hostname
			verda ssh gpu-runner

			# SSH by instance ID
			verda ssh abc-123-def

			# SSH with a specific user and key
			verda ssh gpu-runner --user ubuntu --key ~/.ssh/id_ed25519

			# Pass extra ssh arguments
			verda ssh gpu-runner -- -L 8080:localhost:8080
		`),
		Args:               cobra.ArbitraryArgs, // extra args after "--" are passed to ssh
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			var target string
			var extraArgs []string

			if len(args) > 0 {
				if dash := cmd.Flags().ArgsLenAtDash(); dash >= 0 && dash < len(args) {
					extraArgs = args[dash:]
					args = args[:dash]
				}
				if len(args) > 0 {
					target = args[0]
				}
			}

			return runSSH(cmd, f, ioStreams, opts, target, extraArgs)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.User, "user", "u", opts.User, "SSH user")
	flags.StringVarP(&opts.KeyFile, "key", "i", "", "Path to SSH identity file")

	return cmd
}

func runSSH(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *sshOptions, target string, extraArgs []string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Resolving instance...")
	}
	instances, err := client.Instances.Get(ctx, "")
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	// Interactive picker when no target is provided.
	if target == "" {
		running := filterRunning(instances)
		if len(running) == 0 {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "No running instances found.")
			return nil
		}

		labels := make([]string, 0, len(running)+1)
		for i := range running {
			ip := ""
			if running[i].IP != nil && *running[i].IP != "" {
				ip = *running[i].IP
			}
			labels = append(labels, fmt.Sprintf("%-20s  %-18s  %s  %s",
				running[i].Hostname,
				running[i].InstanceType,
				running[i].Location,
				ip,
			))
		}
		labels = append(labels, "Cancel")

		idx, err := f.Prompter().Select(cmd.Context(), "Select instance to SSH into", labels)
		if err != nil {
			return nil // User pressed Esc/Ctrl+C during prompt.
		}
		if idx == len(running) {
			return nil // Cancel selected.
		}
		target = running[idx].Hostname
	}

	inst := resolveInstance(instances, target)
	if inst == nil {
		return fmt.Errorf("instance %q not found", target)
	}

	if inst.Status != verda.StatusRunning {
		return fmt.Errorf("instance %q is not running (status: %s)", inst.Hostname, inst.Status)
	}

	if inst.IP == nil || *inst.IP == "" {
		return fmt.Errorf("instance %q has no IP address assigned", inst.Hostname)
	}

	ip := *inst.IP

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh not found in PATH: %w", err)
	}

	sshArgs := []string{"ssh"}
	if opts.KeyFile != "" {
		sshArgs = append(sshArgs, "-i", opts.KeyFile)
	}
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", opts.User, ip))
	sshArgs = append(sshArgs, extraArgs...)

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "SSH command:", sshArgs)

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "Connecting to %s (%s)...\n", inst.Hostname, ip)

	return syscall.Exec(sshPath, sshArgs, os.Environ()) //nolint:gosec // Intentional: replace process with ssh using user-provided host and args.
}

// filterRunning returns only instances with status running.
func filterRunning(instances []verda.Instance) []verda.Instance {
	var running []verda.Instance
	for i := range instances {
		if instances[i].Status == verda.StatusRunning {
			running = append(running, instances[i])
		}
	}
	return running
}

// resolveInstance finds an instance by exact ID match first, then by hostname.
func resolveInstance(instances []verda.Instance, target string) *verda.Instance {
	// Exact ID match.
	for i := range instances {
		if instances[i].ID == target {
			return &instances[i]
		}
	}
	// Hostname match (case-insensitive).
	for i := range instances {
		if strings.EqualFold(instances[i].Hostname, target) {
			return &instances[i]
		}
	}
	return nil
}
