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

package template

import (
	tmpl "github.com/verda-cloud/verda-cli/internal/verda-cli/template"
)

// Re-export types from the shared template package so that existing
// code in cmd/template continues to compile without changes.
type (
	Template    = tmpl.Template
	StorageSpec = tmpl.StorageSpec
	Entry       = tmpl.Entry
)

// Re-export functions from the shared template package.
var (
	ValidateName          = tmpl.ValidateName
	Save                  = tmpl.Save
	Load                  = tmpl.Load
	LoadFromPath          = tmpl.LoadFromPath
	Resolve               = tmpl.Resolve
	List                  = tmpl.List
	ListAll               = tmpl.ListAll
	Delete                = tmpl.Delete
	ExpandHostnamePattern = tmpl.ExpandHostnamePattern
)
