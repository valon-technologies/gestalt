package main

import (
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/providers/bigquery"
	"github.com/valon-technologies/gestalt/plugins/providers/extend"
	"github.com/valon-technologies/gestalt/plugins/providers/figma"
	"github.com/valon-technologies/gestalt/plugins/providers/gong"
	"github.com/valon-technologies/gestalt/plugins/providers/jira"
	"github.com/valon-technologies/gestalt/plugins/providers/ramp"
	"github.com/valon-technologies/gestalt/plugins/providers/rippling"
)

func registerProviders(f *bootstrap.FactoryRegistry) {
	f.Providers["bigquery"] = bigquery.Factory
	f.Providers["extend"] = extend.Factory
	f.Providers["figma"] = figma.Factory
	f.Providers["gong"] = gong.Factory
	f.Providers["jira"] = jira.Factory
	f.Providers["ramp"] = ramp.Factory
	f.Providers["rippling"] = rippling.Factory
}
