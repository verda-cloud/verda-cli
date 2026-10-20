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
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

var deploymentNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validateDeploymentName(name string) error {
	switch {
	case name == "":
		return errors.New("deployment name is required")
	case len(name) > 63:
		return fmt.Errorf("deployment name %q is longer than 63 characters", name)
	case !deploymentNameRE.MatchString(name):
		return fmt.Errorf("deployment name %q must be lowercase alphanumerics and hyphens, starting and ending with an alphanumeric", name)
	}
	return nil
}

func rejectLatestTag(image string) error {
	if verda.IsLatestTag(image) {
		return fmt.Errorf("container image %q must use a specific tag, not ':latest'", image)
	}
	return nil
}

const (
	envTypePlain     = "plain"
	envTypeSecret    = "secret"
	mountTypeScratch = "scratch"
	mountTypeSecret  = "secret"
)

var envVarNameRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func parseEnvFlag(entry, envType string) (verda.ContainerEnvVar, error) {
	eq := strings.IndexByte(entry, '=')
	if eq < 1 {
		return verda.ContainerEnvVar{}, fmt.Errorf("invalid env entry %q: expected KEY=VALUE", entry)
	}
	name := entry[:eq]
	value := entry[eq+1:]
	if !envVarNameRE.MatchString(name) {
		return verda.ContainerEnvVar{}, fmt.Errorf("invalid env name %q: use uppercase letters, digits, and underscores, not leading with a digit", name)
	}
	return verda.ContainerEnvVar{
		Type:                     envType,
		Name:                     name,
		ValueOrReferenceToSecret: value,
	}, nil
}

func buildEnvVars(plain, secret []string) ([]verda.ContainerEnvVar, error) {
	total := len(plain) + len(secret)
	if total == 0 {
		return nil, nil
	}
	out := make([]verda.ContainerEnvVar, 0, total)
	for _, e := range plain {
		v, err := parseEnvFlag(e, envTypePlain)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	for _, e := range secret {
		v, err := parseEnvFlag(e, envTypeSecret)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func parseSecretMountFlag(entry string) (verda.ContainerVolumeMount, error) {
	colon := strings.IndexByte(entry, ':')
	if colon < 1 || colon == len(entry)-1 {
		return verda.ContainerVolumeMount{}, fmt.Errorf("invalid secret mount %q: expected SECRET:MOUNT_PATH", entry)
	}
	secretName := entry[:colon]
	mountPath := entry[colon+1:]
	if !strings.HasPrefix(mountPath, "/") {
		return verda.ContainerVolumeMount{}, fmt.Errorf("invalid secret mount %q: mount path must be absolute", entry)
	}
	return verda.ContainerVolumeMount{
		Type:       mountTypeSecret,
		MountPath:  mountPath,
		SecretName: secretName,
	}, nil
}

func buildVolumeMounts(secretMounts []string) ([]verda.ContainerVolumeMount, error) {
	mounts := []verda.ContainerVolumeMount{
		{Type: mountTypeScratch, MountPath: defaultGeneralStoragePath},
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

func resolveQueueLoad(preset string, custom int) (int, error) {
	if custom > 0 {
		if custom > 1000 {
			return 0, errors.New("--queue-load must be in 1..1000")
		}
		return custom, nil
	}
	switch strings.ToLower(preset) {
	case presetInstant:
		return queueLoadInstant, nil
	case presetBalanced, "":
		return queueLoadBalanced, nil
	case presetCostSaver, "cost_saver", "costsaver":
		return queueLoadCostSaver, nil
	case presetCustom:
		return 0, errors.New("--queue-preset=custom requires --queue-load")
	default:
		return 0, fmt.Errorf("invalid --queue-preset %q: expected instant, balanced, cost-saver, or custom", preset)
	}
}

func isPromptCancel(err error) bool {
	return cmdutil.IsPromptCancel(err)
}
