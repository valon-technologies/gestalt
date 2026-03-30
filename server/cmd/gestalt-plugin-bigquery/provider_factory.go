//go:build !functionaltest

package main

import (
	"github.com/valon-technologies/gestalt/server/core"
	providerbigquery "github.com/valon-technologies/gestalt/server/internal/drivers/providers/bigquery"
)

func providerForRun() (core.Provider, error) {
	return providerbigquery.NewQueryProvider(), nil
}
