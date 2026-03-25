//go:build !functionaltest

package main

import (
	"github.com/valon-technologies/gestalt/core"
	providerbigquery "github.com/valon-technologies/gestalt/plugins/providers/bigquery"
)

func providerForRun() (core.Provider, error) {
	return providerbigquery.NewQueryProvider(), nil
}
