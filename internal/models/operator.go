package models

const (
	StateAnnotation = "nullzen.ai/state"
)

const (
	CreatingEvent = "creating"
	CreatedEvent  = "created"
	UpdatingEvent = "updating"
	UpdatedEvent  = "updated"
	DeletingEvent = "deleting"
	DeletedEvent  = "deleted"
	GenericEvent  = "generic"
)

const (
	DeletionFinalizer = "nullzen.ai/deletionFinalizer"
)
