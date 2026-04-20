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

package util

import "github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

// InstanceStatusMessages maps raw API instance statuses to human-friendly messages.
var InstanceStatusMessages = map[string]string{
	verda.StatusNew:          "Creating instance...",
	verda.StatusOrdered:      "Instance ordered...",
	verda.StatusProvisioning: "Provisioning instance...",
	verda.StatusValidating:   "Validating instance...",
	verda.StatusPending:      "Waiting for capacity...",
	verda.StatusRunning:      "Running",
	verda.StatusOffline:      "Offline",
	verda.StatusError:        "Error",
	verda.StatusDiscontinued: "Discontinued",
	verda.StatusNotFound:     "Not found",
	verda.StatusNoCapacity:   "No capacity",
	verda.StatusDeleting:     "Deleting...",
}

// InstanceTerminalStatuses contains statuses where polling should stop.
var InstanceTerminalStatuses = map[string]bool{
	verda.StatusRunning:      true,
	verda.StatusOffline:      true,
	verda.StatusError:        true,
	verda.StatusDiscontinued: true,
	verda.StatusNotFound:     true,
	verda.StatusNoCapacity:   true,
}

// VolumeTerminalStatuses contains volume statuses where polling should stop.
var VolumeTerminalStatuses = map[string]bool{
	"attached": true,
	"detached": true,
}

// InstanceStatusMessage returns a human-friendly message for an instance status.
// Falls back to the raw status string if no mapping exists.
func InstanceStatusMessage(status string) string {
	if msg, ok := InstanceStatusMessages[status]; ok {
		return msg
	}
	return status
}
