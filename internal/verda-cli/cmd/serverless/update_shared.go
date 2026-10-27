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

package serverless

import (
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func anyFlagChanged(cmd *cobra.Command, flags ...string) bool {
	for _, f := range flags {
		if cmd.Flags().Changed(f) {
			return true
		}
	}
	return false
}

func computeFromCurrent(c *verda.ContainerCompute) verda.ContainerCompute {
	if c == nil {
		return verda.ContainerCompute{}
	}
	return verda.ContainerCompute{Name: c.Name, Size: c.Size}
}

// mergeEnvVarsForUpdate rebuilds the env-var list. Changed types are replaced
// from the flag values; unchanged types are carried over from current.
func mergeEnvVarsForUpdate(envChanged, envSecretChanged bool, envFlags, envSecretFlags []string, current []verda.ContainerEnvVar) ([]verda.ContainerEnvVar, error) {
	var result []verda.ContainerEnvVar

	if envChanged {
		for _, e := range envFlags {
			v, err := parseEnvFlag(e, envTypePlain)
			if err != nil {
				return nil, err
			}
			result = append(result, v)
		}
	} else {
		for _, e := range current {
			if e.Type == envTypePlain {
				result = append(result, e)
			}
		}
	}

	if envSecretChanged {
		for _, e := range envSecretFlags {
			v, err := parseEnvFlag(e, envTypeSecret)
			if err != nil {
				return nil, err
			}
			result = append(result, v)
		}
	} else {
		for _, e := range current {
			if e.Type == envTypeSecret {
				result = append(result, e)
			}
		}
	}

	return result, nil
}

// mergeVolumeMountsForUpdate keeps non-secret mounts (scratch) and replaces
// secret mounts with the new set.
func mergeVolumeMountsForUpdate(secretMounts []string, current []verda.ContainerVolumeMount) ([]verda.ContainerVolumeMount, error) {
	var mounts []verda.ContainerVolumeMount
	for _, m := range current {
		if m.Type != mountTypeSecret {
			mounts = append(mounts, m)
		}
	}
	for _, entry := range secretMounts {
		m, err := parseSecretMountFlag(entry)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, m)
	}
	return mounts, nil
}

// mergeEntrypointForUpdate merges entrypoint/cmd overrides. Changed fields
// replace; unchanged fields are carried over from current.
func mergeEntrypointForUpdate(entrypointChanged, cmdChanged bool, entrypoint, cmdArgs []string, current *verda.ContainerEntrypointOverrides) *verda.ContainerEntrypointOverrides {
	ep := &verda.ContainerEntrypointOverrides{}
	if current != nil {
		ep.Entrypoint = current.Entrypoint
		ep.Cmd = current.Cmd
	}
	if entrypointChanged {
		ep.Entrypoint = entrypoint
	}
	if cmdChanged {
		ep.Cmd = cmdArgs
	}
	ep.Enabled = len(ep.Entrypoint) > 0 || len(ep.Cmd) > 0
	if !ep.Enabled {
		return nil
	}
	return ep
}
