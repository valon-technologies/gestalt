package main

import (
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/providers/bigquery"
	"github.com/valon-technologies/gestalt/server/internal/drivers/providers/jira"
)

func registerProviders(f *bootstrap.FactoryRegistry) {
	f.Providers["bigquery"] = bigquery.Factory
	f.Providers["jira"] = jira.Factory
}
