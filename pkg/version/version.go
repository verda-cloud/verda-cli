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

// Package version provides build-time version information for binaries.
//
// Set the build variables via -ldflags:
//
//	go build -ldflags "-X github.com/verda-cloud/verda-cli/pkg/version.gitVersion=v1.0.0 \
//	  -X github.com/verda-cloud/verda-cli/pkg/version.gitCommit=$(git rev-parse HEAD) \
//	  -X github.com/verda-cloud/verda-cli/pkg/version.gitTreeState=clean \
//	  -X github.com/verda-cloud/verda-cli/pkg/version.buildDate=$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
package version

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
)

const (
	unknownValue = "unknown"
	trueValue    = "true"
)

// Build-time variables set via -ldflags.
var (
	gitVersion   = "v0.0.0-dev"
	gitCommit    = unknownValue
	gitTreeState = unknownValue
	buildDate    = unknownValue
)

// Info holds the version information for a binary.
type Info struct {
	GitVersion   string `json:"gitVersion"`
	GitCommit    string `json:"gitCommit"`
	GitTreeState string `json:"gitTreeState"`
	BuildDate    string `json:"buildDate"`
	GoVersion    string `json:"goVersion"`
	Compiler     string `json:"compiler"`
	Platform     string `json:"platform"`
}

// Get returns the version information populated from build-time variables
// and runtime values.
func Get() Info {
	return Info{
		GitVersion:   gitVersion,
		GitCommit:    gitCommit,
		GitTreeState: gitTreeState,
		BuildDate:    buildDate,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// GetFromDebugInfo returns version information extracted from Go's embedded
// debug build info. Useful when ldflags are not set (e.g. `go install`).
func GetFromDebugInfo(modulePath string) Info {
	info := Get()

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}

	if info.GitVersion == "v0.0.0-dev" {
		for _, dep := range bi.Deps {
			if dep.Path == modulePath {
				info.GitVersion = dep.Version
				break
			}
		}
		if info.GitVersion == "v0.0.0-dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.GitVersion = bi.Main.Version
		}
	}

	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			if info.GitCommit == unknownValue {
				info.GitCommit = setting.Value
			}
		case "vcs.modified":
			if info.GitTreeState == unknownValue {
				if setting.Value == trueValue {
					info.GitTreeState = "dirty"
				} else {
					info.GitTreeState = "clean"
				}
			}
		case "vcs.time":
			if info.BuildDate == unknownValue {
				info.BuildDate = setting.Value
			}
		}
	}

	return info
}

// String returns the git version string.
func (i Info) String() string {
	return i.GitVersion
}

// ToJSON returns the version info as a JSON string.
func (i Info) ToJSON() string {
	b, _ := json.Marshal(i)
	return string(b)
}

// Text returns a human-readable multi-line version summary.
func (i Info) Text() string {
	return fmt.Sprintf(`gitVersion:   %s
gitCommit:    %s
gitTreeState: %s
buildDate:    %s
goVersion:    %s
compiler:     %s
platform:     %s`,
		i.GitVersion, i.GitCommit, i.GitTreeState,
		i.BuildDate, i.GoVersion, i.Compiler, i.Platform)
}
