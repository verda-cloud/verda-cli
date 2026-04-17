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

package vm

import (
	"context"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// fetchInstances loads instances from the API with an optional status filter,
// showing a spinner while loading.
func fetchInstances(ctx context.Context, f cmdutil.Factory, client *verda.Client, statusFilter ...string) ([]verda.Instance, error) {
	apiStatus := ""
	if len(statusFilter) == 1 {
		apiStatus = statusFilter[0]
	}

	instances, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading instances...", func() ([]verda.Instance, error) {
		return client.Instances.Get(ctx, apiStatus)
	})
	if err != nil {
		return nil, err
	}

	if len(statusFilter) > 1 {
		instances = filterByStatus(instances, statusFilter)
	}

	return instances, nil
}

// filterByStatus returns only instances whose Status matches one of the given statuses.
func filterByStatus(instances []verda.Instance, statuses []string) []verda.Instance {
	filtered := make([]verda.Instance, 0, len(instances))
	for i := range instances {
		for _, s := range statuses {
			if instances[i].Status == s { //nolint:gosec // i is bounded by range
				filtered = append(filtered, instances[i]) //nolint:gosec // i is bounded by range
				break
			}
		}
	}
	return filtered
}
