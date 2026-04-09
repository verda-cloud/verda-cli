package vm

import (
	"context"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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
			if instances[i].Status == s {
				filtered = append(filtered, instances[i])
				break
			}
		}
	}
	return filtered
}
