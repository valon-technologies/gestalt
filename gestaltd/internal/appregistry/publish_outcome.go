package appregistry

// ObjectWriteOutcome reports whether an immutable registry object was created or skipped.
type ObjectWriteOutcome string

const (
	ObjectWriteOutcomeUploaded ObjectWriteOutcome = "uploaded"
	ObjectWriteOutcomeSkipped  ObjectWriteOutcome = "skipped"
)

// CatalogWriteOutcome reports whether a mutable catalog was updated or left unchanged.
type CatalogWriteOutcome string

const (
	CatalogWriteOutcomeUpdated      CatalogWriteOutcome = "updated"
	CatalogWriteOutcomeUnchanged    CatalogWriteOutcome = "unchanged"
	CatalogWriteOutcomeNotAttempted CatalogWriteOutcome = "not_attempted"
)

// ImmutableObjectOutcome records one immutable object's publish result.
type ImmutableObjectOutcome struct {
	StorageURL string
	Outcome    ObjectWriteOutcome
}

// ImmutablePublishOutcome summarizes immutable artifact and entry uploads.
type ImmutablePublishOutcome struct {
	Artifacts []ImmutableObjectOutcome
	Entry     ImmutableObjectOutcome
}

// PublishResult summarizes the committed outcome of each publish phase.
type PublishResult struct {
	Artifacts []ImmutableObjectOutcome
	Entry     ImmutableObjectOutcome
	Retention CatalogWriteOutcome
	Index     CatalogWriteOutcome
}
