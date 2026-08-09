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

package log

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestNewOptions_Defaults(t *testing.T) {
	opts := NewOptions()

	if opts.Level != "info" {
		t.Errorf("expected level 'info', got %q", opts.Level)
	}
	if opts.Format != "console" {
		t.Errorf("expected format 'console', got %q", opts.Format)
	}
	if len(opts.OutputPaths) != 1 || opts.OutputPaths[0] != "stdout" {
		t.Errorf("expected output paths [stdout], got %v", opts.OutputPaths)
	}
	if opts.DisableCaller {
		t.Error("DisableCaller should default to false")
	}
	if opts.DisableStacktrace {
		t.Error("DisableStacktrace should default to false")
	}
	if opts.EnableColor {
		t.Error("EnableColor should default to false")
	}
}

func TestOptions_Validate(t *testing.T) {
	opts := NewOptions()
	errs := opts.Validate()
	if len(errs) != 0 {
		t.Errorf("expected no validation errors for defaults, got %v", errs)
	}
}

func TestOptions_AddFlags(t *testing.T) {
	opts := NewOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(fs)

	expectedFlags := []string{
		"log.level",
		"log.disable-caller",
		"log.disable-stacktrace",
		"log.enable-color",
		"log.format",
		"log.output-paths",
	}

	for _, name := range expectedFlags {
		if fs.Lookup(name) == nil {
			t.Errorf("expected flag %q to be registered", name)
		}
	}
}

func TestOptions_AddFlags_ParseLevel(t *testing.T) {
	opts := NewOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(fs)

	if err := fs.Parse([]string{"--log.level=debug"}); err != nil {
		t.Fatalf("flag parsing failed: %v", err)
	}
	if opts.Level != "debug" {
		t.Errorf("expected level 'debug' after parsing, got %q", opts.Level)
	}
}

func TestOptions_AddFlags_ParseFormat(t *testing.T) {
	opts := NewOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(fs)

	if err := fs.Parse([]string{"--log.format=json"}); err != nil {
		t.Fatalf("flag parsing failed: %v", err)
	}
	if opts.Format != "json" {
		t.Errorf("expected format 'json' after parsing, got %q", opts.Format)
	}
}

func TestOptions_AddFlags_ParseBoolFlags(t *testing.T) {
	opts := NewOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(fs)

	if err := fs.Parse([]string{
		"--log.disable-caller",
		"--log.disable-stacktrace",
		"--log.enable-color",
	}); err != nil {
		t.Fatalf("flag parsing failed: %v", err)
	}
	if !opts.DisableCaller {
		t.Error("expected DisableCaller=true after --log.disable-caller")
	}
	if !opts.DisableStacktrace {
		t.Error("expected DisableStacktrace=true after --log.disable-stacktrace")
	}
	if !opts.EnableColor {
		t.Error("expected EnableColor=true after --log.enable-color")
	}
}

func TestOptions_AddFlags_ParseOutputPaths(t *testing.T) {
	opts := NewOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(fs)

	if err := fs.Parse([]string{"--log.output-paths=stdout,/var/log/app.log"}); err != nil {
		t.Fatalf("flag parsing failed: %v", err)
	}
	if len(opts.OutputPaths) != 2 {
		t.Fatalf("expected 2 output paths, got %d: %v", len(opts.OutputPaths), opts.OutputPaths)
	}
	if opts.OutputPaths[0] != "stdout" || opts.OutputPaths[1] != "/var/log/app.log" {
		t.Errorf("unexpected output paths: %v", opts.OutputPaths)
	}
}
