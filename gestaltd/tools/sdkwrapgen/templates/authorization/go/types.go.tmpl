package authorization

import "time"

type Subject struct {
	Type       string
	ID         string
	Properties map[string]any
}

type Action struct {
	Name       string
	Properties map[string]any
}

type Resource struct {
	Type       string
	ID         string
	Properties map[string]any
}

type CheckAccessRequest struct {
	Subject  *Subject
	Action   *Action
	Resource *Resource
}

type CheckAccessResponse struct {
	Allowed bool
	ModelID string
}

type CheckAccessManyRequest struct {
	Requests []*CheckAccessRequest
}

type CheckAccessManyResponse struct {
	Decisions []*CheckAccessResponse
}

type RelationshipTargetType int32

const (
	RelationshipTargetTypeUnspecified RelationshipTargetType = 0
	RelationshipTargetTypeSubject     RelationshipTargetType = 1
	RelationshipTargetTypeResource    RelationshipTargetType = 2
	RelationshipTargetTypeSubjectSet  RelationshipTargetType = 3
)

type SourceLayer int32

const (
	SourceLayerUnspecified  SourceLayer = 0
	SourceLayerStaticConfig SourceLayer = 1
	SourceLayerRuntime      SourceLayer = 2
)

type RelationshipFilter struct {
	Target           RelationshipTarget
	Relation         string
	Resource         *Resource
	TargetType       RelationshipTargetType
	TargetEntityType string
	ResourceType     string
	SourceLayer      SourceLayer
}

type ListRelationshipsRequest struct {
	Filter    *RelationshipFilter
	PageSize  int32
	PageToken string
}

type ListRelationshipsResponse struct {
	Relationships []*Relationship
	NextPageToken string
}

type AddRelationshipRequest struct {
	Relationship *Relationship
}

type AddRelationshipResponse struct {
	Relationship *Relationship
}

type DeleteRelationshipRequest struct {
	RelationshipTuple *RelationshipTuple
}

type DeleteRelationshipResponse struct{}

type SetAuthorizationStateRequest struct {
	Model         *Model
	Relationships []*Relationship
}

type SetAuthorizationStateResponse struct {
	ActiveModel *ModelRef
}

type Relationship struct {
	Tuple       *RelationshipTuple
	Properties  map[string]any
	SourceLayer SourceLayer
}

type RelationshipTuple struct {
	Target   RelationshipTarget
	Relation string
	Resource *Resource
}

type RelationshipTarget interface {
	isRelationshipTarget()
}

type RelationshipTargetSubject struct {
	Subject Subject
}

type RelationshipTargetResource struct {
	Resource Resource
}

type RelationshipTargetSubjectSet struct {
	SubjectSet SubjectSet
}

type RelationshipTargetUnset struct{}

func (RelationshipTargetSubject) isRelationshipTarget()    {}
func (RelationshipTargetResource) isRelationshipTarget()   {}
func (RelationshipTargetSubjectSet) isRelationshipTarget() {}
func (RelationshipTargetUnset) isRelationshipTarget()      {}

func SubjectTarget(subject Subject) RelationshipTarget {
	return RelationshipTargetSubject{Subject: subject}
}

func ResourceTarget(resource Resource) RelationshipTarget {
	return RelationshipTargetResource{Resource: resource}
}

func SubjectSetTarget(subjectSet SubjectSet) RelationshipTarget {
	return RelationshipTargetSubjectSet{SubjectSet: subjectSet}
}

func UnsetRelationshipTarget() RelationshipTarget {
	return RelationshipTargetUnset{}
}

type SubjectSet struct {
	Resource *Resource
	Relation string
}

type Model struct {
	ID            string
	Version       string
	ResourceTypes []*ModelResourceType
}

type DefaultAccessPolicy int32

const (
	DefaultAccessPolicyDeny  DefaultAccessPolicy = 0
	DefaultAccessPolicyAllow DefaultAccessPolicy = 1
)

type ModelResourceType struct {
	Name                string
	Relations           []*ModelRelation
	Actions             []*ModelAction
	SourceLayer         SourceLayer
	DefaultAccessPolicy DefaultAccessPolicy
}

type ModelRelation struct {
	Name           string
	AllowedTargets []ModelAllowedTarget
}

type ModelAction struct {
	Name      string
	Relations []string
}

type ModelAllowedTarget interface {
	isModelAllowedTarget()
}

type ModelAllowedTargetSubjectType struct {
	SubjectType string
}

type ModelAllowedTargetResourceType struct {
	ResourceType string
}

type ModelAllowedTargetSubjectSetType struct {
	SubjectSetType SubjectSetType
}

type ModelAllowedTargetUnset struct{}

func (ModelAllowedTargetSubjectType) isModelAllowedTarget()    {}
func (ModelAllowedTargetResourceType) isModelAllowedTarget()   {}
func (ModelAllowedTargetSubjectSetType) isModelAllowedTarget() {}
func (ModelAllowedTargetUnset) isModelAllowedTarget()          {}

func SubjectTypeTarget(subjectType string) ModelAllowedTarget {
	return ModelAllowedTargetSubjectType{SubjectType: subjectType}
}

func ResourceTypeTarget(resourceType string) ModelAllowedTarget {
	return ModelAllowedTargetResourceType{ResourceType: resourceType}
}

func SubjectSetTypeTarget(subjectSetType SubjectSetType) ModelAllowedTarget {
	return ModelAllowedTargetSubjectSetType{SubjectSetType: subjectSetType}
}

func UnsetModelAllowedTarget() ModelAllowedTarget {
	return ModelAllowedTargetUnset{}
}

type SubjectSetType struct {
	ResourceType string
	Relation     string
}

type ModelRef struct {
	ID        string
	Version   string
	CreatedAt *time.Time
}

type GetActiveModelRefResponse struct {
	Model *ModelRef
}

type SetActiveModelRequest struct {
	Model *Model
}

type SetActiveModelResponse struct {
	Model *ModelRef
}

type ModelResourceTypeFilter struct {
	Name        string
	SourceLayer SourceLayer
}

type ListActiveModelResourceTypesRequest struct {
	Filter    *ModelResourceTypeFilter
	PageSize  int32
	PageToken string
}

type ListActiveModelResourceTypesResponse struct {
	ResourceTypes []*ModelResourceType
	NextPageToken string
	ModelID       string
}
