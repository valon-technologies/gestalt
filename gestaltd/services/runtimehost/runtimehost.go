// Package runtimehost exposes the host-owned runtime process and host-service
// primitives used by executable and hosted provider runtimes.
package runtimehost

import (
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

type TelemetryProviders = metricutil.TelemetryProviders
