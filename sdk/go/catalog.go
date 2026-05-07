package gestalt

import proto "github.com/valon-technologies/gestalt/internal/gen/v1"

// Catalog describes the operations a plugin exposes to Gestalt.
type Catalog = proto.Catalog

// CatalogOperation describes one callable operation in a plugin catalog.
type CatalogOperation = proto.CatalogOperation

// CatalogParameter describes one input parameter in a plugin catalog operation.
type CatalogParameter = proto.CatalogParameter

// OperationAnnotations carries optional host hints about operation behavior.
type OperationAnnotations = proto.OperationAnnotations
