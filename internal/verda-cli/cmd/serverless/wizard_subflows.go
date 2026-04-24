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
	"context"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
)

// promptEnvVar collects one environment-variable entry interactively. Returns
// (nil, nil) on user cancel or empty name so the caller can end the loop.
func promptEnvVar(ctx context.Context, prompter tui.Prompter) (*verda.ContainerEnvVar, error) {
	name, err := prompter.TextInput(ctx, "Env name (e.g. HF_HOME)")
	if err != nil {
		return nil, nil //nolint:nilerr // prompter cancel is a clean exit
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	if !envVarNameRE.MatchString(name) {
		_, _ = prompter.Confirm(ctx, "Invalid env name — use uppercase, digits, underscore, no leading digit. Press Enter to continue.", tui.WithConfirmDefault(true))
		return nil, nil
	}

	value, err := prompter.TextInput(ctx, "Env value")
	if err != nil {
		return nil, nil //nolint:nilerr // prompter cancel is a clean exit
	}
	return &verda.ContainerEnvVar{
		Type:                     envTypePlain,
		Name:                     name,
		ValueOrReferenceToSecret: value,
	}, nil
}

// promptSecretMount asks the user to pick a secret (or file-secret) and a
// mount path. Returns (nil, nil) on cancel/empty to end the loop.
func promptSecretMount(ctx context.Context, prompter tui.Prompter, secrets []verda.Secret, fileSecrets []verda.FileSecret) (*verda.ContainerVolumeMount, error) {
	labels := make([]string, 0, len(secrets)+len(fileSecrets)+1)
	values := make([]string, 0, len(secrets)+len(fileSecrets)+1)
	for _, s := range secrets {
		labels = append(labels, "secret: "+s.Name)
		values = append(values, s.Name)
	}
	for _, s := range fileSecrets {
		labels = append(labels, "file-secret: "+s.Name)
		values = append(values, s.Name)
	}
	labels = append(labels, "Cancel")

	idx, err := prompter.Select(ctx, "Select secret to mount", labels)
	if err != nil || idx == len(labels)-1 {
		return nil, nil //nolint:nilerr // prompter cancel is a clean exit
	}

	mountPath, err := prompter.TextInput(ctx, "Mount path (e.g. /etc/secret/api-key)")
	if err != nil {
		return nil, nil //nolint:nilerr // prompter cancel is a clean exit
	}
	mountPath = strings.TrimSpace(mountPath)
	if !strings.HasPrefix(mountPath, "/") {
		_, _ = prompter.Confirm(ctx, "Mount path must be absolute. Press Enter to continue.", tui.WithConfirmDefault(true))
		return nil, nil
	}
	return &verda.ContainerVolumeMount{
		Type:       mountTypeSecret,
		MountPath:  mountPath,
		SecretName: values[idx],
	}, nil
}
