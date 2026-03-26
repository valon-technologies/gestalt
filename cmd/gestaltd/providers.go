package main

import (
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/providers/bigquery"
	"github.com/valon-technologies/gestalt/plugins/providers/dbt_cloud"
	"github.com/valon-technologies/gestalt/plugins/providers/hex"
	"github.com/valon-technologies/gestalt/plugins/providers/intercom"
	"github.com/valon-technologies/gestalt/plugins/providers/jira"
	"github.com/valon-technologies/gestalt/plugins/providers/notion"
)

func registerProviders(f *bootstrap.FactoryRegistry) {
	f.Providers["bigquery"] = bigquery.Factory
	f.Providers["dbt_cloud"] = dbt_cloud.Factory
	f.Providers["hex"] = hex.Factory
	f.Providers["intercom"] = intercom.Factory
	f.Providers["jira"] = jira.Factory
	f.Providers["notion"] = notion.Factory
}
