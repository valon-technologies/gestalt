package main

import (
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/providers/bigquery"
	"github.com/valon-technologies/gestalt/plugins/providers/jira"
)

func registerProviders(f *bootstrap.FactoryRegistry) {
	f.Providers["bigquery"] = bigquery.Factory
	f.Providers["jira"] = jira.Factory
}
