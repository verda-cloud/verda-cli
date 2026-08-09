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

package tui

// ResolveConfirmConfig applies options to a default ConfirmConfig.
func ResolveConfirmConfig(opts []ConfirmOption) ConfirmConfig {
	cfg := ConfirmConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// ResolveTextInputConfig applies options to a default TextInputConfig.
func ResolveTextInputConfig(opts []TextInputOption) TextInputConfig {
	cfg := TextInputConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// ResolveSelectConfig applies options to a default SelectConfig.
func ResolveSelectConfig(opts []SelectOption) SelectConfig {
	cfg := SelectConfig{PageSize: 10, Loop: true}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// ResolveLiveListConfig applies options to a default LiveListConfig.
func ResolveLiveListConfig(opts []LiveListOption) LiveListConfig {
	cfg := LiveListConfig{PageSize: 10, Loop: true}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// ResolveMultiSelectConfig applies options to a default MultiSelectConfig.
func ResolveMultiSelectConfig(opts []MultiSelectOption) MultiSelectConfig {
	cfg := MultiSelectConfig{PageSize: 10, Loop: true}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// ResolveEditorConfig applies options to a default EditorConfig.
func ResolveEditorConfig(opts []EditorOption) EditorConfig {
	cfg := EditorConfig{FileExt: ".txt", ShowHelp: true}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}
