//go:build functionaltest

package main

import (
	"os"

	"github.com/valon-technologies/gestalt/server/core"
	providerbigquery "github.com/valon-technologies/gestalt/server/internal/drivers/providers/bigquery"
)

func providerForRun() (core.Provider, error) {
	scenario := os.Getenv(functionalTestScenarioEnv)
	if scenario == "" {
		return providerbigquery.NewQueryProvider(), nil
	}
	return providerbigquery.NewQueryProviderForFunctionalTest(scenario)
}
