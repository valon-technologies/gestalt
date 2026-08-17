package server

import (
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func publishSessionLedger(services *coredata.Services) *coredata.AppRegistryPublishSessionService {
	if services == nil {
		return nil
	}
	return services.AppRegistryPublishSessions
}
