//go:build functionaltest

package main

import (
	"os"

	"github.com/valon-technologies/gestalt/core"
	providerbigquery "github.com/valon-technologies/gestalt/plugins/providers/bigquery"
)

func providerForRun() (core.Provider, error) {
	scenario := os.Getenv(functionalTestScenarioEnv)
	if scenario == "" {
		return providerbigquery.NewQueryProvider(), nil
	}
	return providerbigquery.NewQueryProviderForFunctionalTest(scenario)
}
