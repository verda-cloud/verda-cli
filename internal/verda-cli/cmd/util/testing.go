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

import (
	"errors"
	"net/http"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"

	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// TestFactory is a configurable Factory for use in tests. It implements the
// Factory interface with sensible defaults and allows overriding any field.
type TestFactory struct {
	// PrompterOverride sets the prompter. Defaults to tuitest.New().
	PrompterOverride tui.Prompter

	// StatusOverride sets the status. Defaults to nil (no spinners).
	StatusOverride tui.Status

	// ClientOverride sets the Verda client. If nil, VerdaClient() returns ErrNoClient.
	ClientOverride *verda.Client

	// OutputFormatOverride sets the output format. Defaults to "table".
	OutputFormatOverride string

	// DebugOverride enables debug output.
	DebugOverride bool

	// AgentModeOverride enables agent mode.
	AgentModeOverride bool

	// OptionsOverride sets custom options. If nil, sensible defaults are used.
	OptionsOverride *clioptions.Options
}

// ErrNoClient is returned by TestFactory.VerdaClient when no client is configured.
var ErrNoClient = errors.New("test: no Verda client configured")

// NewTestFactory creates a TestFactory with a mock prompter ready for use.
func NewTestFactory(mock *tuitest.Prompter) *TestFactory {
	return &TestFactory{
		PrompterOverride: mock,
	}
}

func (f *TestFactory) ServerAddr() string { return "https://test.verda.com/v1" }

func (f *TestFactory) HTTPClient() *http.Client { return &http.Client{Timeout: 10 * time.Second} }

func (f *TestFactory) Options() *clioptions.Options {
	if f.OptionsOverride != nil {
		return f.OptionsOverride
	}
	return &clioptions.Options{
		Server:  "https://test.verda.com/v1",
		Timeout: 10 * time.Second,
		Output:  f.OutputFormat(),
		Debug:   f.DebugOverride,
	}
}

func (f *TestFactory) VerdaClient() (*verda.Client, error) {
	if f.ClientOverride != nil {
		return f.ClientOverride, nil
	}
	return nil, ErrNoClient
}

func (f *TestFactory) Login() (string, error) { return "", ErrNoClient }

func (f *TestFactory) Token() string { return "" }

func (f *TestFactory) Prompter() tui.Prompter {
	if f.PrompterOverride != nil {
		return f.PrompterOverride
	}
	return tuitest.New()
}

func (f *TestFactory) Status() tui.Status {
	return f.StatusOverride
}

func (f *TestFactory) Debug() bool { return f.DebugOverride }

func (f *TestFactory) OutputFormat() string {
	if f.OutputFormatOverride != "" {
		return f.OutputFormatOverride
	}
	return "table"
}

func (f *TestFactory) AgentMode() bool { return f.AgentModeOverride }
