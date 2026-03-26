package main

import (
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/providers/bigquery"
	"github.com/valon-technologies/gestalt/plugins/providers/datadog"
	"github.com/valon-technologies/gestalt/plugins/providers/incident_io"
	"github.com/valon-technologies/gestalt/plugins/providers/jira"
	"github.com/valon-technologies/gestalt/plugins/providers/launchdarkly"
	"github.com/valon-technologies/gestalt/plugins/providers/pagerduty"
)

func registerProviders(f *bootstrap.FactoryRegistry) {
	f.Providers["bigquery"] = bigquery.Factory
	f.Providers["datadog"] = datadog.Factory
	f.Providers["incident_io"] = incident_io.Factory
	f.Providers["jira"] = jira.Factory
	f.Providers["launchdarkly"] = launchdarkly.Factory
	f.Providers["pagerduty"] = pagerduty.Factory
}
