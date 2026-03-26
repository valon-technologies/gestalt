package main

import (
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/providers/bigquery"
	"github.com/valon-technologies/gestalt/plugins/providers/github"
	"github.com/valon-technologies/gestalt/plugins/providers/github_app"
	"github.com/valon-technologies/gestalt/plugins/providers/gitlab"
	"github.com/valon-technologies/gestalt/plugins/providers/jira"
)

func registerProviders(f *bootstrap.FactoryRegistry) {
	f.Providers["bigquery"] = bigquery.Factory
	f.Providers["github"] = github.Factory
	f.Providers["github_app"] = github_app.Factory
	f.Providers["gitlab"] = gitlab.Factory
	f.Providers["jira"] = jira.Factory
}
