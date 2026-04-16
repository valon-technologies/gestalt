package core

import (
	"errors"
)

var ErrSessionCatalogUnavailable = errors.New("session catalog unavailable")

func SupportsSessionCatalog(prov Provider) bool {
	_, ok := prov.(SessionCatalogProvider)
	return ok
}
