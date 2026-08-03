package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	openfga "github.com/openfga/go-sdk"
	"github.com/openfga/go-sdk/credentials"
	indexeddb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

// OpenFGAConfig configures the deployment-scoped built-in OpenFGA backend.
// Values may already have been resolved from Gestalt secret references.
type OpenFGAConfig struct {
	APIURL       string `yaml:"apiUrl"`
	StoreID      string `yaml:"storeId"`
	TokenIssuer  string `yaml:"tokenIssuer"`
	Audience     string `yaml:"audience"`
	ClientID     string `yaml:"clientId"`
	ClientSecret string `yaml:"clientSecret"`
	Scopes       string `yaml:"scopes,omitempty"`
}

type openFGA struct {
	mu sync.RWMutex

	client  *openfga.APIClient
	storeID string

	model    *proto.AuthorizationModel
	modelRef *proto.AuthorizationModelRef
	codec    *fgaCodec
	meta     map[string]*proto.Relationship
	legacyDB indexeddb.Database
	legacy   []*proto.Relationship
}

type fgaCodec struct {
	model       *proto.AuthorizationModel
	types       map[string]string
	relations   map[string]string
	permissions map[string]string
	reverse     map[string]string
}

func NewOpenFGA(node yaml.Node, databases ...indexeddb.Database) (core.AuthorizationProvider, error) {
	var cfg OpenFGAConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("openfga authorization: decode config: %w", err)
	}
	for name, value := range map[string]string{
		"apiUrl": cfg.APIURL, "storeId": cfg.StoreID, "tokenIssuer": cfg.TokenIssuer,
		"audience": cfg.Audience, "clientId": cfg.ClientID, "clientSecret": cfg.ClientSecret,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("openfga authorization: %s is required", name)
		}
	}
	creds, err := credentials.NewCredentials(credentials.Credentials{
		Method: credentials.CredentialsMethodClientCredentials,
		Config: &credentials.Config{
			ClientCredentialsApiTokenIssuer: cfg.TokenIssuer,
			ClientCredentialsApiAudience:    cfg.Audience,
			ClientCredentialsClientId:       cfg.ClientID,
			ClientCredentialsClientSecret:   cfg.ClientSecret,
			ClientCredentialsScopes:         cfg.Scopes,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openfga authorization: credentials: %w", err)
	}
	apiCfg, err := openfga.NewConfiguration(openfga.Configuration{
		ApiUrl:      strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/"),
		Credentials: creds,
	})
	if err != nil {
		return nil, fmt.Errorf("openfga authorization: client config: %w", err)
	}
	provider := &openFGA{
		client:  openfga.NewAPIClient(apiCfg),
		storeID: strings.TrimSpace(cfg.StoreID),
		meta:    make(map[string]*proto.Relationship),
	}
	if len(databases) > 0 && databases[0] != nil {
		provider.legacyDB = databases[0]
		provider.legacy, _ = loadLegacyRelationships(context.Background(), databases[0])
	}
	return provider, nil
}

func (p *openFGA) Ping(ctx context.Context) error {
	if p == nil || p.client == nil {
		return status.Error(codes.FailedPrecondition, "openfga authorization is not configured")
	}
	_, _, err := p.client.OpenFgaApi.GetStore(ctx, p.storeID).Execute()
	if err != nil {
		return status.Errorf(codes.Unavailable, "openfga store health: %v", err)
	}
	if err := p.syncLegacyRelationships(ctx); err != nil {
		return err
	}
	return nil
}

func (p *openFGA) Close() error { return nil }

func (p *openFGA) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if req == nil || req.Subject == nil || req.Resource == nil || req.Action == nil {
		return nil, status.Error(codes.InvalidArgument, "subject, action, and resource are required")
	}
	p.mu.RLock()
	model := p.model
	codec := p.codec
	modelID := ""
	if p.modelRef != nil {
		modelID = p.modelRef.Id
	}
	p.mu.RUnlock()
	if model == nil || codec == nil {
		return nil, status.Error(codes.FailedPrecondition, "openfga authorization model is not configured")
	}
	if scope := subjectScope(req.Subject); scope != "" && !scopeAllows(scope, req.Resource.Type, req.Action.Name) {
		return &proto.CheckAccessResponse{Allowed: false, ModelId: modelID}, nil
	}
	resourceType := findResourceType(model, req.Resource.Type)
	if resourceType == nil {
		return &proto.CheckAccessResponse{Allowed: false, ModelId: modelID}, nil
	}
	action := findAction(resourceType, req.Action.Name)
	if action == nil {
		action = findAction(resourceType, "*")
	}
	if action == nil {
		return &proto.CheckAccessResponse{Allowed: false, ModelId: modelID}, nil
	}
	permission := codec.permissions[resourceType.Name+"\x00"+action.Name]
	if permission == "" {
		return &proto.CheckAccessResponse{Allowed: false, ModelId: modelID}, nil
	}
	key, err := codec.checkTuple(req.Subject, permission, req.Resource)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "authorization check: %v", err)
	}
	check := openfga.NewCheckRequest(*openfga.NewCheckRequestTupleKey(key.User, key.Relation, key.Object))
	check.SetAuthorizationModelId(codec.model.Id)
	if defaultRole := strings.TrimSpace(resourceType.DefaultRole); defaultRole != "" && contains(action.Relations, defaultRole) {
		role := codec.relations[resourceType.Name+"\x00"+defaultRole]
		if role != "" {
			wildcard := openfga.NewTupleKey(codec.subjectWildcard(req.Subject.Type), role, key.Object)
			check.SetContextualTuples(openfga.ContextualTupleKeys{TupleKeys: []openfga.TupleKey{*wildcard}})
		}
	}
	response, _, err := p.client.OpenFgaApi.Check(ctx, p.storeID).Body(*check).Execute()
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "openfga check: %v", err)
	}
	return &proto.CheckAccessResponse{Allowed: response.GetAllowed(), ModelId: modelID}, nil
}

func (p *openFGA) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	decisions := make([]*proto.CheckAccessResponse, 0, len(req.Requests))
	for _, item := range req.Requests {
		decision, err := p.CheckAccess(ctx, item)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return &proto.CheckAccessManyResponse{Decisions: decisions}, nil
}

func (p *openFGA) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	if req == nil || req.Relationship == nil || req.Relationship.Tuple == nil {
		return nil, status.Error(codes.InvalidArgument, "relationship is required")
	}
	p.mu.RLock()
	codec := p.codec
	p.mu.RUnlock()
	if codec == nil {
		return nil, status.Error(codes.FailedPrecondition, "openfga authorization model is not configured")
	}
	key, err := codec.relationshipTuple(req.Relationship.Tuple)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "relationship: %v", err)
	}
	duplicate := "ignore"
	writes := openfga.WriteRequest{Writes: openfga.NewWriteRequestWrites([]openfga.TupleKey{key}), AuthorizationModelId: stringPtr(codec.model.Id)}
	writes.Writes.OnDuplicate = &duplicate
	if _, _, err := p.client.OpenFgaApi.Write(ctx, p.storeID).Body(writes).Execute(); err != nil {
		return nil, status.Errorf(codes.Unavailable, "openfga relationship write: %v", err)
	}
	copy := cloneRelationship(req.Relationship)
	if copy.SourceLayer == proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
		copy.SourceLayer = proto.SourceLayer_SOURCE_LAYER_RUNTIME
	}
	p.mu.Lock()
	p.meta[tupleKey(copy.Tuple)] = copy
	p.mu.Unlock()
	return &proto.AddRelationshipResponse{Relationship: copy}, nil
}

func (p *openFGA) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	if req == nil || req.RelationshipTuple == nil {
		return nil, status.Error(codes.InvalidArgument, "relationship tuple is required")
	}
	p.mu.RLock()
	codec := p.codec
	p.mu.RUnlock()
	if codec == nil {
		return nil, status.Error(codes.FailedPrecondition, "openfga authorization model is not configured")
	}
	key, err := codec.relationshipTuple(req.RelationshipTuple)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "relationship: %v", err)
	}
	missing := "ignore"
	deletes := openfga.WriteRequest{Deletes: openfga.NewWriteRequestDeletes([]openfga.TupleKeyWithoutCondition{{User: key.User, Relation: key.Relation, Object: key.Object}}), AuthorizationModelId: stringPtr(codec.model.Id)}
	deletes.Deletes.OnMissing = &missing
	if _, _, err := p.client.OpenFgaApi.Write(ctx, p.storeID).Body(deletes).Execute(); err != nil {
		return nil, status.Errorf(codes.Unavailable, "openfga relationship delete: %v", err)
	}
	p.mu.Lock()
	delete(p.meta, tupleKey(req.RelationshipTuple))
	p.mu.Unlock()
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *openFGA) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	if req == nil || req.Model == nil {
		return nil, status.Error(codes.InvalidArgument, "model is required")
	}
	codec, err := newFGACodec(req.Model)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "compile authorization model: %v", err)
	}
	modelRequest, err := codec.modelRequest()
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "compile openfga model: %v", err)
	}
	modelResponse, _, err := p.client.OpenFgaApi.WriteAuthorizationModel(ctx, p.storeID).Body(modelRequest).Execute()
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "openfga write authorization model: %v", err)
	}
	codec.model.Id = modelResponse.GetAuthorizationModelId()
	if err := p.replaceTuples(ctx, codec, req.Relationships); err != nil {
		return nil, err
	}
	ref := &proto.AuthorizationModelRef{Id: req.Model.Id, Version: req.Model.Version, CreatedAt: timestamppb.New(time.Now().UTC())}
	p.mu.Lock()
	p.model = cloneModel(req.Model)
	p.modelRef = ref
	p.codec = codec
	p.meta = make(map[string]*proto.Relationship, len(req.Relationships))
	for _, relationship := range req.Relationships {
		if relationship != nil && relationship.Tuple != nil {
			copy := cloneRelationship(relationship)
			if copy.SourceLayer == proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
				copy.SourceLayer = proto.SourceLayer_SOURCE_LAYER_RUNTIME
			}
			p.meta[tupleKey(copy.Tuple)] = copy
		}
	}
	p.mu.Unlock()
	return &proto.SetAuthorizationStateResponse{ActiveModel: ref}, nil
}

func (p *openFGA) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.modelRef == nil {
		return nil, status.Error(codes.NotFound, "active authorization model is not set")
	}
	return &proto.GetActiveModelRefResponse{Model: cloneModelRef(p.modelRef)}, nil
}

func (p *openFGA) SetActiveModel(_ context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	if req == nil || req.Model == nil {
		return nil, status.Error(codes.InvalidArgument, "model is required")
	}
	p.mu.Lock()
	p.modelRef = &proto.AuthorizationModelRef{Id: req.Model.Id, Version: req.Model.Version, CreatedAt: timestamppb.New(time.Now().UTC())}
	p.mu.Unlock()
	return &proto.SetActiveModelResponse{Model: cloneModelRef(p.modelRef)}, nil
}

func (p *openFGA) ListActiveModelResourceTypes(_ context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.model == nil {
		return nil, status.Error(codes.NotFound, "active authorization model is not set")
	}
	name := ""
	pageSize := 100
	pageToken := 0
	if req != nil {
		if req.Filter != nil {
			name = strings.TrimSpace(req.Filter.Name)
		}
		if req.PageSize > 0 {
			pageSize = int(req.PageSize)
		}
		if req.PageToken != "" {
			parsed, err := strconv.Atoi(req.PageToken)
			if err != nil || parsed < 0 {
				return nil, status.Error(codes.InvalidArgument, "page token is invalid")
			}
			pageToken = parsed
		}
	}
	items := make([]*proto.AuthorizationModelResourceType, 0, len(p.model.ResourceTypes))
	for _, item := range p.model.ResourceTypes {
		if name == "" || item.GetName() == name {
			items = append(items, item)
		}
	}
	if pageToken > len(items) {
		pageToken = len(items)
	}
	end := min(pageToken+pageSize, len(items))
	response := &proto.ListActiveModelResourceTypesResponse{ModelId: p.model.Id}
	for _, item := range items[pageToken:end] {
		response.ResourceTypes = append(response.ResourceTypes, cloneResourceType(item))
	}
	if end < len(items) {
		response.NextPageToken = strconv.Itoa(end)
	}
	return response, nil
}

func (p *openFGA) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	p.mu.RLock()
	codec := p.codec
	model := p.model
	meta := make(map[string]*proto.Relationship, len(p.meta))
	for key, value := range p.meta {
		meta[key] = cloneRelationship(value)
	}
	p.mu.RUnlock()
	if codec == nil || model == nil {
		if legacy, ok := p.legacyRelationships(req); ok {
			return &proto.ListRelationshipsResponse{Relationships: legacy}, nil
		}
		return nil, status.Error(codes.FailedPrecondition, "openfga authorization model is not configured")
	}
	filter := (*proto.RelationshipFilter)(nil)
	pageSize := 100
	pageToken := 0
	if req != nil {
		filter = req.Filter
		if req.PageSize < 0 {
			return nil, status.Error(codes.InvalidArgument, "page size must be non-negative")
		}
		if req.PageSize > 0 {
			pageSize = int(req.PageSize)
		}
		if req.PageToken != "" {
			parsed, err := strconv.Atoi(req.PageToken)
			if err != nil || parsed < 0 {
				return nil, status.Error(codes.InvalidArgument, "page token is invalid")
			}
			pageToken = parsed
		}
	}
	tuples, err := p.readAllTuples(ctx, codec.readFilter(filter))
	if err != nil {
		return nil, err
	}
	items := make([]*proto.Relationship, 0, len(tuples))
	for _, tuple := range tuples {
		relationship, err := codec.relationshipFromTuple(tuple.Key, model, meta)
		if err != nil || !relationshipMatches(filter, relationship) {
			continue
		}
		items = append(items, relationship)
	}
	sort.Slice(items, func(i, j int) bool { return tupleKey(items[i].Tuple) < tupleKey(items[j].Tuple) })
	if pageToken > len(items) {
		pageToken = len(items)
	}
	end := min(pageToken+pageSize, len(items))
	out := &proto.ListRelationshipsResponse{}
	for _, item := range items[pageToken:end] {
		out.Relationships = append(out.Relationships, item)
	}
	if end < len(items) {
		out.NextPageToken = strconv.Itoa(end)
	}
	return out, nil
}

func (p *openFGA) legacyRelationships(req *proto.ListRelationshipsRequest) ([]*proto.Relationship, bool) {
	p.mu.RLock()
	loaded := p.legacy != nil
	legacy := make([]*proto.Relationship, 0, len(p.legacy))
	for _, relationship := range p.legacy {
		if relationshipMatches(req.GetFilter(), relationship) {
			legacy = append(legacy, cloneRelationship(relationship))
		}
	}
	p.mu.RUnlock()
	if !loaded {
		return nil, false
	}
	sort.Slice(legacy, func(i, j int) bool { return tupleKey(legacy[i].Tuple) < tupleKey(legacy[j].Tuple) })
	return legacy, true
}

func (p *openFGA) syncLegacyRelationships(ctx context.Context) error {
	p.mu.RLock()
	db := p.legacyDB
	codec := p.codec
	model := p.model
	legacy := append([]*proto.Relationship(nil), p.legacy...)
	p.mu.RUnlock()
	if db == nil || codec == nil || model == nil {
		return nil
	}
	latest, err := loadLegacyRelationships(ctx, db)
	if err != nil {
		return err
	}
	if len(latest) == 0 {
		latest = legacy
	}
	for _, relationship := range latest {
		if relationship == nil || relationship.Tuple == nil {
			continue
		}
		key, err := codec.relationshipTuple(relationship.Tuple)
		if err != nil {
			continue
		}
		duplicate := "ignore"
		body := openfga.WriteRequest{Writes: openfga.NewWriteRequestWrites([]openfga.TupleKey{key}), AuthorizationModelId: stringPtr(codec.model.Id)}
		body.Writes.OnDuplicate = &duplicate
		if _, _, err := p.client.OpenFgaApi.Write(ctx, p.storeID).Body(body).Execute(); err != nil {
			return status.Errorf(codes.Unavailable, "openfga legacy relationship sync: %v", err)
		}
		p.mu.Lock()
		p.meta[tupleKey(relationship.Tuple)] = cloneRelationship(relationship)
		p.mu.Unlock()
	}
	p.mu.Lock()
	p.legacy = latest
	p.mu.Unlock()
	return nil
}

func (p *openFGA) readAllTuples(ctx context.Context, filter *openfga.ReadRequestTupleKey) ([]openfga.Tuple, error) {
	var tuples []openfga.Tuple
	continuation := ""
	for {
		request := openfga.ReadRequest{PageSize: int32Ptr(100)}
		if filter != nil {
			request.TupleKey = filter
		}
		if continuation != "" {
			request.ContinuationToken = stringPtr(continuation)
		}
		response, _, err := p.client.OpenFgaApi.Read(ctx, p.storeID).Body(request).Execute()
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "openfga read relationships: %v", err)
		}
		tuples = append(tuples, response.GetTuples()...)
		continuation = response.GetContinuationToken()
		if continuation == "" {
			return tuples, nil
		}
	}
}

func (p *openFGA) replaceTuples(ctx context.Context, codec *fgaCodec, relationships []*proto.Relationship) error {
	desired := make(map[string]openfga.TupleKey, len(relationships))
	for _, relationship := range relationships {
		if relationship == nil || relationship.Tuple == nil {
			continue
		}
		key, err := codec.relationshipTuple(relationship.Tuple)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "relationship: %v", err)
		}
		desired[key.User+"\x00"+key.Relation+"\x00"+key.Object] = key
	}
	readTuples, err := p.readAllTuples(ctx, nil)
	if err != nil {
		return err
	}
	var deletes []openfga.TupleKeyWithoutCondition
	for _, tuple := range readTuples {
		key := tuple.Key.User + "\x00" + tuple.Key.Relation + "\x00" + tuple.Key.Object
		if _, ok := desired[key]; !ok {
			deletes = append(deletes, openfga.TupleKeyWithoutCondition{User: tuple.Key.User, Relation: tuple.Key.Relation, Object: tuple.Key.Object})
		}
	}
	keys := make([]openfga.TupleKey, 0, len(desired))
	for _, key := range desired {
		keys = append(keys, key)
	}
	for start := 0; start < len(keys) || start < len(deletes); start += 100 {
		endWrites := min(start+100, len(keys))
		endDeletes := min(start+100, len(deletes))
		body := openfga.WriteRequest{AuthorizationModelId: stringPtr(codec.model.Id)}
		if start < len(keys) {
			duplicate := "ignore"
			body.Writes = openfga.NewWriteRequestWrites(keys[start:endWrites])
			body.Writes.OnDuplicate = &duplicate
		}
		if start < len(deletes) {
			missing := "ignore"
			body.Deletes = openfga.NewWriteRequestDeletes(deletes[start:endDeletes])
			body.Deletes.OnMissing = &missing
		}
		if _, _, err := p.client.OpenFgaApi.Write(ctx, p.storeID).Body(body).Execute(); err != nil {
			return status.Errorf(codes.Unavailable, "openfga replace relationships: %v", err)
		}
	}
	return nil
}

func newFGACodec(model *proto.AuthorizationModel) (*fgaCodec, error) {
	codec := &fgaCodec{model: cloneModel(model), types: map[string]string{}, relations: map[string]string{}, permissions: map[string]string{}, reverse: map[string]string{}}
	for _, resourceType := range model.GetResourceTypes() {
		if resourceType == nil || strings.TrimSpace(resourceType.Name) == "" {
			continue
		}
		encoded := stableFGAName("t_", resourceType.Name)
		if previous := codec.reverse[encoded]; previous != "" && previous != resourceType.Name {
			return nil, fmt.Errorf("type encoding collision between %q and %q", previous, resourceType.Name)
		}
		codec.types[resourceType.Name] = encoded
		codec.reverse[encoded] = resourceType.Name
		if strings.TrimSpace(resourceType.DefaultRole) != "" {
			codec.addType("subject")
		}
		for _, relation := range resourceType.Relations {
			if relation == nil {
				continue
			}
			for _, allowed := range relation.AllowedTargets {
				if allowed == nil {
					continue
				}
				switch {
				case allowed.GetSubjectType() != "":
					codec.addType(allowed.GetSubjectType())
				case allowed.GetResourceType() != "":
					codec.addType(allowed.GetResourceType())
				case allowed.GetSubjectSetType() != nil:
					codec.addType(allowed.GetSubjectSetType().ResourceType)
				}
			}
			codec.relations[resourceType.Name+"\x00"+relation.Name] = stableFGAName("r_", resourceType.Name+"\x00"+relation.Name)
		}
		for _, action := range resourceType.Actions {
			if action == nil {
				continue
			}
			codec.permissions[resourceType.Name+"\x00"+action.Name] = stableFGAName("p_", resourceType.Name+"\x00"+action.Name)
		}
	}
	return codec, nil
}

func (c *fgaCodec) addType(logical string) {
	logical = strings.TrimSpace(logical)
	if logical == "" || c.types[logical] != "" {
		return
	}
	encoded := stableFGAName("t_", logical)
	c.types[logical] = encoded
	c.reverse[encoded] = logical
}

func (c *fgaCodec) modelRequest() (openfga.WriteAuthorizationModelRequest, error) {
	definitions := make([]openfga.TypeDefinition, 0, len(c.types))
	defined := make(map[string]struct{}, len(c.model.ResourceTypes))
	for _, resourceType := range c.model.ResourceTypes {
		if resourceType == nil {
			continue
		}
		relations := map[string]openfga.Userset{}
		metadata := map[string]openfga.RelationMetadata{}
		for _, relation := range resourceType.Relations {
			if relation == nil {
				continue
			}
			refs := make([]openfga.RelationReference, 0, len(relation.AllowedTargets)+1)
			for _, target := range relation.AllowedTargets {
				if target == nil {
					continue
				}
				switch {
				case target.GetSubjectType() != "":
					refs = append(refs, openfga.RelationReference{Type: c.types[target.GetSubjectType()]})
				case target.GetResourceType() != "":
					refs = append(refs, openfga.RelationReference{Type: c.types[target.GetResourceType()]})
				case target.GetSubjectSetType() != nil:
					set := target.GetSubjectSetType()
					relationName := c.relations[set.ResourceType+"\x00"+set.Relation]
					refs = append(refs, openfga.RelationReference{Type: c.types[set.ResourceType], Relation: stringPtr(relationName)})
				}
			}
			if strings.TrimSpace(resourceType.DefaultRole) == strings.TrimSpace(relation.Name) {
				refs = append(refs, openfga.RelationReference{Type: c.types["subject"], Wildcard: &map[string]interface{}{}})
			}
			direct := map[string]interface{}{}
			relations[c.relations[resourceType.Name+"\x00"+relation.Name]] = openfga.Userset{This: &direct}
			metadata[c.relations[resourceType.Name+"\x00"+relation.Name]] = openfga.RelationMetadata{DirectlyRelatedUserTypes: &refs}
		}
		for _, action := range resourceType.Actions {
			if action == nil {
				continue
			}
			children := make([]openfga.Userset, 0, len(action.Relations))
			for _, relation := range action.Relations {
				role := c.relations[resourceType.Name+"\x00"+relation]
				if role == "" {
					continue
				}
				object := c.types[resourceType.Name]
				children = append(children, openfga.Userset{ComputedUserset: &openfga.ObjectRelation{Object: &object, Relation: &role}})
			}
			if len(children) == 1 {
				relations[c.permissions[resourceType.Name+"\x00"+action.Name]] = children[0]
			} else if len(children) > 1 {
				relations[c.permissions[resourceType.Name+"\x00"+action.Name]] = openfga.Userset{Union: &openfga.Usersets{Child: children}}
			}
		}
		typeName := c.types[resourceType.Name]
		definitions = append(definitions, openfga.TypeDefinition{Type: typeName, Relations: &relations, Metadata: &openfga.Metadata{Relations: &metadata}})
		defined[resourceType.Name] = struct{}{}
	}
	logicalTypes := make([]string, 0, len(c.types))
	for logical := range c.types {
		if _, ok := defined[logical]; !ok {
			logicalTypes = append(logicalTypes, logical)
		}
	}
	sort.Strings(logicalTypes)
	for _, logical := range logicalTypes {
		relations := map[string]openfga.Userset{}
		definitions = append(definitions, openfga.TypeDefinition{Type: c.types[logical], Relations: &relations})
	}
	return openfga.WriteAuthorizationModelRequest{TypeDefinitions: definitions, SchemaVersion: "1.1"}, nil
}

func (c *fgaCodec) relationshipTuple(tuple *proto.RelationshipTuple) (openfga.TupleKey, error) {
	if tuple == nil || tuple.Resource == nil || tuple.Target == nil {
		return openfga.TupleKey{}, fmt.Errorf("resource and target are required")
	}
	objectType := c.types[tuple.Resource.Type]
	role := c.relations[tuple.Resource.Type+"\x00"+tuple.Relation]
	if objectType == "" || role == "" {
		return openfga.TupleKey{}, fmt.Errorf("unknown resource type or relation")
	}
	user, err := c.targetUser(tuple.Target)
	if err != nil {
		return openfga.TupleKey{}, err
	}
	return openfga.TupleKey{User: user, Relation: role, Object: objectType + ":" + tuple.Resource.Id}, nil
}

func (c *fgaCodec) checkTuple(subject *proto.Subject, permission string, resource *proto.Resource) (openfga.TupleKey, error) {
	if subject == nil || resource == nil {
		return openfga.TupleKey{}, fmt.Errorf("subject and resource are required")
	}
	objectType := c.types[resource.Type]
	if objectType == "" {
		return openfga.TupleKey{}, fmt.Errorf("unknown resource type %q", resource.Type)
	}
	return openfga.TupleKey{User: c.subjectUser(subject), Relation: permission, Object: objectType + ":" + resource.Id}, nil
}

func (c *fgaCodec) targetUser(target *proto.RelationshipTarget) (string, error) {
	if subject := target.GetSubject(); subject != nil {
		return c.subjectUser(subject), nil
	}
	if resource := target.GetResource(); resource != nil {
		typeName := c.types[resource.Type]
		if typeName == "" {
			return "", fmt.Errorf("unknown target resource type %q", resource.Type)
		}
		return typeName + ":" + resource.Id, nil
	}
	if set := target.GetSubjectSet(); set != nil && set.Resource != nil {
		typeName := c.types[set.Resource.Type]
		relation := c.relations[set.Resource.Type+"\x00"+set.Relation]
		if typeName == "" || relation == "" {
			return "", fmt.Errorf("unknown subject set resource type or relation")
		}
		return typeName + ":" + set.Resource.Id + "#" + relation, nil
	}
	return "", fmt.Errorf("relationship target is required")
}

func (c *fgaCodec) subjectUser(subject *proto.Subject) string {
	return c.types[subject.Type] + ":" + subject.Id
}

func (c *fgaCodec) subjectWildcard(subjectType string) string {
	return c.types[subjectType] + ":*"
}

func (c *fgaCodec) readFilter(filter *proto.RelationshipFilter) *openfga.ReadRequestTupleKey {
	if filter == nil {
		return nil
	}
	key := &openfga.ReadRequestTupleKey{}
	if filter.Resource != nil {
		if objectType := c.types[filter.Resource.Type]; objectType != "" {
			value := objectType + ":" + filter.Resource.Id
			key.Object = &value
		}
	}
	// OpenFGA's Read API only accepts exact object IDs, not a type prefix.
	// Leave resource-type-only filtering unbounded and apply it locally after
	// reading the tuples.
	if filter.Relation != "" {
		if relation := c.relationsForFilter(filter); relation != "" {
			key.Relation = &relation
		}
	}
	return key
}

func (c *fgaCodec) relationsForFilter(filter *proto.RelationshipFilter) string {
	if filter.Resource == nil {
		return ""
	}
	return c.relations[filter.Resource.Type+"\x00"+filter.Relation]
}

func (c *fgaCodec) relationshipFromTuple(key openfga.TupleKey, model *proto.AuthorizationModel, meta map[string]*proto.Relationship) (*proto.Relationship, error) {
	objectType, objectID, ok := splitFGAObject(key.Object)
	if !ok {
		return nil, fmt.Errorf("invalid object %q", key.Object)
	}
	resourceType := c.reverse[objectType]
	relation := c.logicalRelation(resourceType, key.Relation)
	if resourceType == "" || relation == "" {
		return nil, fmt.Errorf("unknown encoded tuple")
	}
	target, err := c.targetFromUser(key.User, resourceType, relation, model)
	if err != nil {
		return nil, err
	}
	tuple := &proto.RelationshipTuple{Resource: &proto.Resource{Type: resourceType, Id: objectID}, Relation: relation, Target: target}
	if existing := meta[tupleKey(tuple)]; existing != nil {
		return cloneRelationship(existing), nil
	}
	return &proto.Relationship{Tuple: tuple, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, nil
}

func (c *fgaCodec) logicalRelation(resourceType, encoded string) string {
	for key, value := range c.relations {
		if strings.HasPrefix(key, resourceType+"\x00") && value == encoded {
			return strings.TrimPrefix(key, resourceType+"\x00")
		}
	}
	return ""
}

func (c *fgaCodec) targetFromUser(user, resourceType, relation string, model *proto.AuthorizationModel) (*proto.RelationshipTarget, error) {
	if strings.Contains(user, "#") {
		parts := strings.SplitN(user, "#", 2)
		setType, setID, ok := splitFGAObject(parts[0])
		if !ok {
			return nil, fmt.Errorf("invalid subject set %q", user)
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{Resource: &proto.Resource{Type: c.reverse[setType], Id: setID}, Relation: c.logicalRelation(c.reverse[setType], parts[1])}}}, nil
	}
	typeName, id, ok := splitFGAObject(user)
	if !ok {
		return nil, fmt.Errorf("invalid target %q", user)
	}
	logicalType := c.reverse[typeName]
	if targetLooksLikeResource(model, resourceType, relation, logicalType) {
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Resource{Resource: &proto.Resource{Type: logicalType, Id: id}}}, nil
	}
	return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: logicalType, Id: id}}}, nil
}

func stableFGAName(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + hex.EncodeToString(sum[:])[:26]
}

type legacyRelationship struct {
	Tuple       *legacyRelationshipTuple `json:"tuple"`
	Properties  map[string]any           `json:"properties,omitempty"`
	SourceLayer json.RawMessage          `json:"source_layer"`
}

type legacyRelationshipTuple struct {
	Target   *legacyRelationshipTarget `json:"target"`
	Relation string                    `json:"relation"`
	Resource *legacyEntity             `json:"resource"`
}

type legacyRelationshipTarget struct {
	Subject    *legacyEntity     `json:"subject,omitempty"`
	Resource   *legacyEntity     `json:"resource,omitempty"`
	SubjectSet *legacySubjectSet `json:"subject_set,omitempty"`
}

type legacySubjectSet struct {
	Resource *legacyEntity `json:"resource"`
	Relation string        `json:"relation"`
}

type legacyEntity struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties,omitempty"`
}

func loadLegacyRelationships(ctx context.Context, db indexeddb.Database) ([]*proto.Relationship, error) {
	if db == nil {
		return nil, nil
	}
	records, err := db.ObjectStore("authz_relationships").GetAll(ctx, nil)
	if err != nil {
		return nil, nil
	}
	relationships := make([]*proto.Relationship, 0, len(records))
	for _, record := range records {
		data, err := json.Marshal(record["value"])
		if err != nil {
			return nil, fmt.Errorf("decode legacy relationship: %w", err)
		}
		var stored legacyRelationship
		if err := json.Unmarshal(data, &stored); err != nil {
			return nil, fmt.Errorf("decode legacy relationship: %w", err)
		}
		relationship, err := legacyRelationshipToProto(&stored)
		if err != nil {
			return nil, err
		}
		relationships = append(relationships, relationship)
	}
	return relationships, nil
}

func legacyRelationshipToProto(stored *legacyRelationship) (*proto.Relationship, error) {
	if stored == nil || stored.Tuple == nil || stored.Tuple.Resource == nil || stored.Tuple.Target == nil {
		return nil, fmt.Errorf("legacy relationship is incomplete")
	}
	target := &proto.RelationshipTarget{}
	switch {
	case stored.Tuple.Target.Subject != nil:
		entity := stored.Tuple.Target.Subject
		target.Kind = &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: entity.Type, Id: entity.ID, Properties: legacyProperties(entity.Properties)}}
	case stored.Tuple.Target.Resource != nil:
		entity := stored.Tuple.Target.Resource
		target.Kind = &proto.RelationshipTarget_Resource{Resource: &proto.Resource{Type: entity.Type, Id: entity.ID, Properties: legacyProperties(entity.Properties)}}
	case stored.Tuple.Target.SubjectSet != nil && stored.Tuple.Target.SubjectSet.Resource != nil:
		entity := stored.Tuple.Target.SubjectSet.Resource
		target.Kind = &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{Resource: &proto.Resource{Type: entity.Type, Id: entity.ID, Properties: legacyProperties(entity.Properties)}, Relation: stored.Tuple.Target.SubjectSet.Relation}}
	default:
		return nil, fmt.Errorf("legacy relationship target is incomplete")
	}
	resource := stored.Tuple.Resource
	return &proto.Relationship{
		Tuple:       &proto.RelationshipTuple{Target: target, Relation: stored.Tuple.Relation, Resource: &proto.Resource{Type: resource.Type, Id: resource.ID, Properties: legacyProperties(resource.Properties)}},
		Properties:  legacyProperties(stored.Properties),
		SourceLayer: legacySourceLayer(stored.SourceLayer),
	}, nil
}

func legacyProperties(properties map[string]any) *structpb.Struct {
	if len(properties) == 0 {
		return nil
	}
	value, err := structpb.NewStruct(properties)
	if err != nil {
		return nil
	}
	return value
}

func legacySourceLayer(raw json.RawMessage) proto.SourceLayer {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		switch strings.TrimSpace(value) {
		case "static_config", "staticconfig":
			return proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG
		case "runtime":
			return proto.SourceLayer_SOURCE_LAYER_RUNTIME
		}
	}
	var number int32
	if json.Unmarshal(raw, &number) == nil {
		return proto.SourceLayer(number)
	}
	return proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED
}

func splitFGAObject(value string) (string, string, bool) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], parts[0] != "" && parts[1] != ""
}

func tupleKey(tuple *proto.RelationshipTuple) string {
	if tuple == nil || tuple.Resource == nil || tuple.Target == nil {
		return ""
	}
	target := ""
	if subject := tuple.Target.GetSubject(); subject != nil {
		target = "subject:" + subject.Type + ":" + subject.Id
	} else if resource := tuple.Target.GetResource(); resource != nil {
		target = "resource:" + resource.Type + ":" + resource.Id
	} else if set := tuple.Target.GetSubjectSet(); set != nil && set.Resource != nil {
		target = "set:" + set.Resource.Type + ":" + set.Resource.Id + "#" + set.Relation
	}
	return tuple.Resource.Type + "\x00" + tuple.Resource.Id + "\x00" + tuple.Relation + "\x00" + target
}

func relationshipMatches(filter *proto.RelationshipFilter, relationship *proto.Relationship) bool {
	if filter == nil || relationship == nil || relationship.Tuple == nil {
		return relationship != nil
	}
	tuple := relationship.Tuple
	if filter.Target != nil && !gproto.Equal(filter.Target, tuple.Target) {
		return false
	}
	if filter.Relation != "" && filter.Relation != tuple.Relation {
		return false
	}
	if filter.ResourceType != "" && (tuple.Resource == nil || filter.ResourceType != tuple.Resource.Type) {
		return false
	}
	if filter.Resource != nil && (tuple.Resource == nil || filter.Resource.Type != tuple.Resource.Type || filter.Resource.Id != tuple.Resource.Id) {
		return false
	}
	if filter.SourceLayer != proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED && relationship.SourceLayer != filter.SourceLayer {
		return false
	}
	if filter.TargetType != proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_UNSPECIFIED {
		if targetType(relationship.Tuple.Target) != filter.TargetType {
			return false
		}
	}
	if filter.TargetEntityType != "" && targetEntityType(relationship.Tuple.Target) != filter.TargetEntityType {
		return false
	}
	return true
}

func targetLooksLikeResource(model *proto.AuthorizationModel, resourceType, relation, targetType string) bool {
	for _, item := range model.ResourceTypes {
		if item == nil || item.Name != resourceType {
			continue
		}
		for _, rel := range item.Relations {
			if rel == nil || rel.Name != relation {
				continue
			}
			for _, allowed := range rel.AllowedTargets {
				if allowed.GetResourceType() == targetType {
					return true
				}
			}
		}
	}
	return false
}

func targetType(target *proto.RelationshipTarget) proto.RelationshipTargetType {
	switch {
	case target.GetSubject() != nil:
		return proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_SUBJECT
	case target.GetResource() != nil:
		return proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_RESOURCE
	case target.GetSubjectSet() != nil:
		return proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_SUBJECT_SET
	default:
		return proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_UNSPECIFIED
	}
}

func targetEntityType(target *proto.RelationshipTarget) string {
	if target.GetSubject() != nil {
		return target.GetSubject().Type
	}
	if target.GetResource() != nil {
		return target.GetResource().Type
	}
	if target.GetSubjectSet() != nil && target.GetSubjectSet().Resource != nil {
		return target.GetSubjectSet().Resource.Type
	}
	return ""
}

func subjectScope(subject *proto.Subject) string {
	if subject == nil || subject.Properties == nil {
		return ""
	}
	value, ok := subject.Properties.Fields["scope"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(value.GetStringValue())
}

func scopeAllows(scope, providerName, operationName string) bool {
	for _, token := range strings.Fields(scope) {
		if token == providerName || token == providerName+":"+operationName {
			return true
		}
	}
	return false
}

func findResourceType(model *proto.AuthorizationModel, name string) *proto.AuthorizationModelResourceType {
	for _, resourceType := range model.ResourceTypes {
		if resourceType != nil && resourceType.Name == name {
			return resourceType
		}
	}
	return nil
}

func findAction(resourceType *proto.AuthorizationModelResourceType, name string) *proto.ModelAction {
	for _, action := range resourceType.Actions {
		if action != nil && action.Name == name {
			return action
		}
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func cloneRelationship(in *proto.Relationship) *proto.Relationship {
	if in == nil {
		return nil
	}
	return gproto.Clone(in).(*proto.Relationship)
}

func cloneModel(in *proto.AuthorizationModel) *proto.AuthorizationModel {
	if in == nil {
		return nil
	}
	return gproto.Clone(in).(*proto.AuthorizationModel)
}

func cloneModelRef(in *proto.AuthorizationModelRef) *proto.AuthorizationModelRef {
	if in == nil {
		return nil
	}
	out := &proto.AuthorizationModelRef{Id: in.Id, Version: in.Version}
	if in.CreatedAt != nil {
		out.CreatedAt = timestamppb.New(in.CreatedAt.AsTime())
	}
	return out
}

func cloneResourceType(in *proto.AuthorizationModelResourceType) *proto.AuthorizationModelResourceType {
	if in == nil {
		return nil
	}
	return gproto.Clone(in).(*proto.AuthorizationModelResourceType)
}

func stringPtr(value string) *string { return &value }
func int32Ptr(value int32) *int32    { return &value }
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure the compatibility implementation keeps the existing provider contract.
var _ core.AuthorizationProvider = (*openFGA)(nil)
