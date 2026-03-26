package main

import (
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/providers/bigquery"
	"github.com/valon-technologies/gestalt/plugins/providers/google_calendar"
	"github.com/valon-technologies/gestalt/plugins/providers/google_docs"
	"github.com/valon-technologies/gestalt/plugins/providers/google_drive"
	"github.com/valon-technologies/gestalt/plugins/providers/google_sheets"
	"github.com/valon-technologies/gestalt/plugins/providers/jira"
)

func registerProviders(f *bootstrap.FactoryRegistry) {
	f.Providers["bigquery"] = bigquery.Factory
	f.Providers["google_calendar"] = google_calendar.Factory
	f.Providers["google_docs"] = google_docs.Factory
	f.Providers["google_drive"] = google_drive.Factory
	f.Providers["google_sheets"] = google_sheets.Factory
	f.Providers["jira"] = jira.Factory
}
