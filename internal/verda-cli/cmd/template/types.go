package template

import (
	tmpl "github/verda-cloud/verda-cli/internal/verda-cli/template"
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
