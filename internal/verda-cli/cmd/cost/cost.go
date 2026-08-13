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

package cost

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdCost creates the parent cost command.
func NewCmdCost(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Cost estimates, pricing, and account balance",
		Long: cmdutil.LongDesc(`
			Estimate costs, view pricing, and check account balance.

			Figures here are estimates built from catalog prices, for planning.
			The web console is the authority on what you are charged: it
			accounts for credits, discounts and contract terms this CLI cannot
			see. Where the two differ, the web console is right.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		newCmdEstimate(f, ioStreams),
		newCmdRunning(f, ioStreams),
		newCmdBalance(f, ioStreams),
	)

	return cmd
}
