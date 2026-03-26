package main

import (
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/providers/bigquery"
	"github.com/valon-technologies/gestalt/plugins/providers/jira"
	"github.com/valon-technologies/gestalt/plugins/providers/linear"
	"github.com/valon-technologies/gestalt/plugins/providers/linear_app"
)

func registerProviders(f *bootstrap.FactoryRegistry) {
	f.Providers["bigquery"] = bigquery.Factory
	f.Providers["jira"] = jira.Factory
	f.Providers["linear"] = linear.Factory
	f.Providers["linear_app"] = linear_app.Factory
}
