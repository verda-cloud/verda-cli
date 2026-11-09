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

package docgen

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newTestRoot() *cobra.Command {
	run := func(*cobra.Command, []string) {}
	root := &cobra.Command{Use: "verda"}
	root.PersistentFlags().StringP("output", "o", "table", "Output format")
	root.PersistentFlags().String("secret", "", "Hidden global flag")
	_ = root.PersistentFlags().MarkHidden("secret")

	vm := &cobra.Command{Use: "vm", Short: "Manage virtual machines"}
	vmList := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List VMs", Run: run,
		Example: "  # All VMs\n  verda vm list"}
	vmList.Flags().String("status", "", "Filter by status")
	vm.AddCommand(vmList)

	container := &cobra.Command{
		Use:         "container",
		Short:       "Manage containers",
		Annotations: map[string]string{cmdutil.TagAnnotation: "beta"},
		Long:        "Deploy a <name> with *.csv input.\n\nBash:\n  source <(verda completion bash)\n\n  # once:\n  verda completion bash > /etc/x\n\nDone.",
	}
	create := &cobra.Command{Use: "create <name>", Short: "Create a | deployment", Run: run}
	internal := &cobra.Command{Use: "internal", Short: "Hidden subcommand", Hidden: true, Run: run}
	container.AddCommand(create, internal)
	batchjob := &cobra.Command{Use: "batchjob", Short: "Hidden top-level", Hidden: true, Run: run}

	cmdutil.CommandGroups{
		{Message: "VM Commands:", Commands: []*cobra.Command{vm}},
		{Message: "Serverless Commands:", Commands: []*cobra.Command{container, batchjob}},
	}.Add(root)
	return root
}

func generate(t *testing.T) map[string]string {
	t.Helper()
	files, err := Generate(newTestRoot())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	out := map[string]string{}
	for name, b := range files {
		out[name] = string(b)
	}
	return out
}

func TestGenerate_Files(t *testing.T) {
	t.Parallel()
	got := slices.Sorted(maps.Keys(generate(t)))
	want := []string{"commands.md", "reference/container.md", "reference/vm.md"}
	if !slices.Equal(got, want) {
		t.Errorf("files = %v, want %v (hidden commands get no page)", got, want)
	}
}

func TestGenerate_Index(t *testing.T) {
	t.Parallel()
	index := generate(t)[IndexFile]

	vmAt, serverlessAt := strings.Index(index, "## VM Commands"), strings.Index(index, "## Serverless Commands")
	if vmAt < 0 || serverlessAt < vmAt {
		t.Errorf("groups should follow declared order; got:\n%s", index)
	}
	for _, want := range []string{
		generatedHeader,
		"| [`verda container`](reference/container.md) (beta) | Manage containers |",
		"| [`verda vm list`](reference/vm.md#verda-vm-list) | List VMs |",
		`Create a \| deployment`,
		"-o, --output string",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index missing %q; got:\n%s", want, index)
		}
	}
	for _, unwanted := range []string{"batchjob", "internal", "--secret"} {
		if strings.Contains(index, unwanted) {
			t.Errorf("index must not mention hidden %q; got:\n%s", unwanted, index)
		}
	}
}

// verda --help lists a group's commands as declared; the index must match, not sort them.
func TestGenerate_IndexKeepsDeclarationOrder(t *testing.T) {
	t.Parallel()
	run := func(*cobra.Command, []string) {}
	root := &cobra.Command{Use: "verda"}
	groups := cmdutil.CommandGroups{{Message: "VM Commands:", Commands: []*cobra.Command{
		{Use: "vm", Short: "Manage virtual machines", Run: run},
		{Use: "ssh", Short: "SSH into a VM", Run: run},
	}}}
	groups.Add(root)
	cmdutil.SetUsageTemplate(root, groups)

	files, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	index := string(files[IndexFile])
	vmAt, sshAt := strings.Index(index, "[`verda vm`]"), strings.Index(index, "[`verda ssh`]")
	if vmAt < 0 || sshAt < vmAt {
		t.Errorf("index should list vm before ssh, as declared; got:\n%s", index)
	}
}

func TestGenerate_Page(t *testing.T) {
	t.Parallel()
	files := generate(t)
	page := files["reference/vm.md"]
	for _, want := range []string{
		"# verda vm\n",
		"- [`verda vm list`](#verda-vm-list): List VMs",
		"## verda vm list\n",
		"Aliases: `ls`",
		"--status string",
		"```bash\n# All VMs\nverda vm list\n```",
		"../commands.md#global-flags",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("vm page missing %q; got:\n%s", want, page)
		}
	}
	if strings.Contains(page, "-o, --output") {
		t.Errorf("global flags belong on the index only; got:\n%s", page)
	}
	if strings.Contains(files["reference/container.md"], "internal") {
		t.Errorf("hidden subcommand must be skipped")
	}
}

func TestGenerate_LongText(t *testing.T) {
	t.Parallel()
	page := generate(t)["reference/container.md"]
	for _, want := range []string{
		"# verda container (beta)",
		`Deploy a &lt;name&gt; with \*.csv input.`,
		"```text\nsource <(verda completion bash)\n\n# once:\nverda completion bash > /etc/x\n```\n\nDone.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("container page missing %q; got:\n%s", want, page)
		}
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	t.Parallel()
	first, err := Generate(newTestRoot())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(newTestRoot())
	if err != nil {
		t.Fatal(err)
	}
	if !maps.EqualFunc(first, second, bytes.Equal) {
		t.Error("two runs over equal trees must produce identical output")
	}
}

// Rendering resolves (and so mutates) flag state; a rerun on the same tree must not differ.
func TestGenerate_IdempotentOnSameRoot(t *testing.T) {
	t.Parallel()
	root := newTestRoot()
	first, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.EqualFunc(first, second, bytes.Equal) {
		t.Error("a second run over the same root must produce identical output")
	}
}

func TestGenerate_UsageShowsInheritedFlags(t *testing.T) {
	t.Parallel()
	root := &cobra.Command{Use: "verda"}
	root.PersistentFlags().String("output", "table", "Output format")
	root.AddCommand(&cobra.Command{Use: "locations", Short: "List locations", Run: func(*cobra.Command, []string) {}})
	files, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if page := string(files["reference/locations.md"]); !strings.Contains(page, "verda locations [flags]") {
		t.Errorf("usage should include [flags] from global flags, as --help does; got:\n%s", page)
	}
}

// registryLsSection renders a tree whose registry group declares persistent flags
// via addFlags, and returns the "verda registry ls" section of its page.
func registryLsSection(t *testing.T, addFlags func(*pflag.FlagSet)) string {
	t.Helper()
	root := &cobra.Command{Use: "verda"}
	root.PersistentFlags().StringP("output", "o", "table", "Output format")
	registry := &cobra.Command{Use: "registry", Short: "Manage the registry"}
	addFlags(registry.PersistentFlags())
	ls := &cobra.Command{Use: "ls", Short: "List repositories", Run: func(*cobra.Command, []string) {}}
	ls.Flags().Bool("all", false, "Include empty repositories")
	registry.AddCommand(ls)
	root.AddCommand(registry)

	files, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["reference/registry.md"])
	_, section, ok := strings.Cut(page, "## verda registry ls\n")
	if !ok {
		t.Fatalf("page has no registry ls section; got:\n%s", page)
	}
	return section
}

func TestGenerate_InheritedFlags(t *testing.T) {
	t.Parallel()
	section := registryLsSection(t, func(fs *pflag.FlagSet) {
		fs.String("project", "", "Registry project")
	})
	local, inherited, ok := strings.Cut(section, "**Inherited flags**")
	if !ok {
		t.Fatalf("a group's persistent flag should be listed under Inherited flags; got:\n%s", section)
	}
	if !strings.Contains(local, "--all") {
		t.Errorf("local flag --all should be listed under Flags; got:\n%s", section)
	}
	if !strings.Contains(inherited, "--project string") {
		t.Errorf("group flag --project should be listed under Inherited flags; got:\n%s", section)
	}
	if strings.Contains(section, "-o, --output") {
		t.Errorf("global flags belong on the index only; got:\n%s", section)
	}
}

// Cobra gives a subcommand the nearest parent's flag of a name, so a group flag
// that shadows a global one is not the global flag and must be listed.
func TestGenerate_InheritedFlagShadowsGlobal(t *testing.T) {
	t.Parallel()
	section := registryLsSection(t, func(fs *pflag.FlagSet) {
		fs.String("output", "json", "Registry output format")
	})
	_, inherited, ok := strings.Cut(section, "**Inherited flags**")
	if !ok || !strings.Contains(inherited, `Registry output format (default "json")`) {
		t.Errorf("the group's --output should be listed under Inherited flags; got:\n%s", section)
	}
}

func TestGenerate_RejectsANSI(t *testing.T) {
	t.Parallel()
	root := &cobra.Command{Use: "verda"}
	root.AddCommand(&cobra.Command{Use: "status", Short: "\x1b[1mStatus\x1b[0m", Run: func(*cobra.Command, []string) {}})
	if _, err := Generate(root); err == nil {
		t.Error("expected an error for ANSI escapes in help text")
	}
}

func TestEscapeText(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"plain":                 "plain",
		"copy <src> <dst>":      "copy &lt;src&gt; &lt;dst&gt;",
		"run `verda <cmd>` now": "run `verda <cmd>` now",
		"a & b * c":             `a &amp; b \* c`,
		"unclosed `<x>":         "unclosed `&lt;x&gt;",
	}
	for in, want := range tests {
		if got := escapeText(in); got != want {
			t.Errorf("escapeText(%q) = %q, want %q", in, got, want)
		}
	}
}
