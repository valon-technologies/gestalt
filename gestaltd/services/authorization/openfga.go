package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	openfga "github.com/openfga/go-sdk"
	"github.com/openfga/go-sdk/credentials"
	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
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
}

type fgaCodec struct {
	model       *proto.AuthorizationModel
	types       map[string]string
	relations   map[string]string
	permissions map[string]string
	reverse     map[string]string
}

func NewOpenFGA(node yaml.Node) (core.AuthorizationProvider, error) {
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
	return &openFGA{
		client:  openfga.NewAPIClient(apiCfg),
		storeID: strings.TrimSpace(cfg.StoreID),
		meta:    make(map[string]*proto.Relationship),
	}, nil
}

func (p *openFGA) Ping(ctx context.Context) error {
	if p == nil || p.client == nil {
		return status.Error(codes.FailedPrecondition, "openfga authorization is not configured")
	}
	_, httpResponse, err := p.client.OpenFgaApi.GetStore(ctx, p.storeID).Execute()
	if httpResponse != nil && httpResponse.Body != nil {
		_ = httpResponse.Body.Close()
	}
	if err != nil {
		return status.Errorf(codes.Unavailable, "openfga store health: %v", err)
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
	if scope := subjectScope(req.Subject); scope != "" && !scopeAllows(scope, req.Resource.Id, req.Action.Name) {
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
	// Match the IndexedDB provider's compatibility contract: DefaultRole is
	// an implicit grant for actions that include it. Scope and action checks
	// above still apply, so a scoped subject can deny the request before this
	// compatibility grant is considered.
	if defaultRole := strings.TrimSpace(resourceType.DefaultRole); defaultRole != "" && contains(action.Relations, defaultRole) {
		return &proto.CheckAccessResponse{Allowed: true, ModelId: modelID}, nil
	}
	key, err := codec.checkTuple(req.Subject, permission, req.Resource)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "authorization check: %v", err)
	}
	check := openfga.NewCheckRequest(*openfga.NewCheckRequestTupleKey(key.User, key.Relation, key.Object))
	check.SetAuthorizationModelId(codec.model.Id)
	response, httpResponse, err := p.client.OpenFgaApi.Check(ctx, p.storeID).Body(*check).Execute()
	if httpResponse != nil && httpResponse.Body != nil {
		_ = httpResponse.Body.Close()
	}
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
	_, httpResponse, err := p.client.OpenFgaApi.Write(ctx, p.storeID).Body(writes).Execute()
	if httpResponse != nil && httpResponse.Body != nil {
		_ = httpResponse.Body.Close()
	}
	if err != nil {
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
	_, httpResponse, err := p.client.OpenFgaApi.Write(ctx, p.storeID).Body(deletes).Execute()
	if httpResponse != nil && httpResponse.Body != nil {
		_ = httpResponse.Body.Close()
	}
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "openfga relationship delete: %v", err)
	}
	p.mu.Lock()
	delete(p.meta, tupleKey(req.RelationshipTuple))
	p.mu.Unlock()
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *openFGA) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return p.setAuthorizationState(ctx, req, true)
}

// BootstrapAuthorizationState publishes the configured authorization model and
// ensures its static relationships exist without reconciling the OpenFGA tuple
// store. OpenFGA is authoritative for runtime relationships after cutover, so
// startup must never delete or recreate them from another backend.
func (p *openFGA) BootstrapAuthorizationState(ctx context.Context, model *proto.AuthorizationModel, relationships []*proto.Relationship) error {
	_, err := p.setAuthorizationState(ctx, &proto.SetAuthorizationStateRequest{
		Model:         model,
		Relationships: relationships,
	}, false)
	return err
}

func (p *openFGA) setAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest, replace bool) (*proto.SetAuthorizationStateResponse, error) {
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
	modelResponse, httpResponse, err := p.client.OpenFgaApi.WriteAuthorizationModel(ctx, p.storeID).Body(modelRequest).Execute()
	if httpResponse != nil && httpResponse.Body != nil {
		_ = httpResponse.Body.Close()
	}
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "openfga write authorization model: %v", err)
	}
	codec.model.Id = modelResponse.GetAuthorizationModelId()
	if err := p.writeAuthorizationRelationships(ctx, codec, req.Relationships, replace); err != nil {
		return nil, err
	}
	ref := &proto.AuthorizationModelRef{Id: req.Model.Id, Version: req.Model.Version, CreatedAt: timestamppb.New(time.Now().UTC())}
	p.mu.Lock()
	p.model = cloneModel(req.Model)
	p.modelRef = ref
	p.codec = codec
	if replace {
		p.meta = make(map[string]*proto.Relationship, len(req.Relationships))
	} else if p.meta == nil {
		p.meta = make(map[string]*proto.Relationship)
	}
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

func (p *openFGA) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	if req == nil || req.Model == nil {
		return nil, status.Error(codes.InvalidArgument, "model is required")
	}
	state, err := p.setAuthorizationState(ctx, &proto.SetAuthorizationStateRequest{Model: req.Model}, false)
	if err != nil {
		return nil, err
	}
	return &proto.SetActiveModelResponse{Model: cloneModelRef(state.ActiveModel)}, nil
}

func (p *openFGA) ListActiveModelResourceTypes(_ context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.model == nil {
		return nil, status.Error(codes.NotFound, "active authorization model is not set")
	}
	name := ""
	sourceLayer := proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED
	pageSize := 100
	pageToken := 0
	if req != nil {
		if req.Filter != nil {
			name = strings.TrimSpace(req.Filter.Name)
			sourceLayer = req.Filter.SourceLayer
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
		if item == nil {
			continue
		}
		if name != "" && item.GetName() != name {
			continue
		}
		if sourceLayer != proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED && item.GetSourceLayer() != sourceLayer {
			continue
		}
		items = append(items, item)
	}
	if pageToken > len(items) {
		pageToken = len(items)
	}
	end := minInt(pageToken+pageSize, len(items))
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
	readFilter, err := codec.readFilter(filter)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "relationship filter: %v", err)
	}
	tuples, err := p.readAllTuples(ctx, readFilter)
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
	end := minInt(pageToken+pageSize, len(items))
	out := &proto.ListRelationshipsResponse{Relationships: append([]*proto.Relationship(nil), items[pageToken:end]...)}
	if end < len(items) {
		out.NextPageToken = strconv.Itoa(end)
	}
	return out, nil
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
		response, httpResponse, err := p.client.OpenFgaApi.Read(ctx, p.storeID).Body(request).Execute()
		if httpResponse != nil && httpResponse.Body != nil {
			_ = httpResponse.Body.Close()
		}
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

func (p *openFGA) writeAuthorizationRelationships(ctx context.Context, codec *fgaCodec, relationships []*proto.Relationship, replace bool) error {
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
	var deletes []openfga.TupleKeyWithoutCondition
	if replace {
		readTuples, err := p.readAllTuples(ctx, nil)
		if err != nil {
			return err
		}
		for _, tuple := range readTuples {
			key := tuple.Key.User + "\x00" + tuple.Key.Relation + "\x00" + tuple.Key.Object
			if _, ok := desired[key]; !ok {
				deletes = append(deletes, openfga.TupleKeyWithoutCondition{User: tuple.Key.User, Relation: tuple.Key.Relation, Object: tuple.Key.Object})
			}
		}
	}
	keys := make([]openfga.TupleKey, 0, len(desired))
	for _, key := range desired {
		keys = append(keys, key)
	}
	for _, batch := range fgaWriteBatches(keys, deletes, 100) {
		body := openfga.WriteRequest{AuthorizationModelId: stringPtr(codec.model.Id)}
		if len(batch.writes) > 0 {
			duplicate := "ignore"
			body.Writes = openfga.NewWriteRequestWrites(batch.writes)
			body.Writes.OnDuplicate = &duplicate
		}
		if len(batch.deletes) > 0 {
			missing := "ignore"
			body.Deletes = openfga.NewWriteRequestDeletes(batch.deletes)
			body.Deletes.OnMissing = &missing
		}
		_, httpResponse, err := p.client.OpenFgaApi.Write(ctx, p.storeID).Body(body).Execute()
		if httpResponse != nil && httpResponse.Body != nil {
			_ = httpResponse.Body.Close()
		}
		if err != nil {
			return status.Errorf(codes.Unavailable, "openfga write relationships: %v", err)
		}
	}
	return nil
}

type fgaWriteBatch struct {
	writes  []openfga.TupleKey
	deletes []openfga.TupleKeyWithoutCondition
}

func fgaWriteBatches(writes []openfga.TupleKey, deletes []openfga.TupleKeyWithoutCondition, limit int) []fgaWriteBatch {
	if limit <= 0 {
		return nil
	}
	batches := make([]fgaWriteBatch, 0, (len(writes)+len(deletes)+limit-1)/limit)
	writeIndex, deleteIndex := 0, 0
	for writeIndex < len(writes) || deleteIndex < len(deletes) {
		remaining := limit
		writeEnd := minInt(writeIndex+remaining, len(writes))
		remaining -= writeEnd - writeIndex
		deleteEnd := minInt(deleteIndex+remaining, len(deletes))
		batches = append(batches, fgaWriteBatch{
			writes:  writes[writeIndex:writeEnd],
			deletes: deletes[deleteIndex:deleteEnd],
		})
		writeIndex, deleteIndex = writeEnd, deleteEnd
	}
	return batches
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
				// A computed userset is evaluated on the current object. OpenFGA's
				// model language requires the object field to be omitted here.
				children = append(children, openfga.Userset{ComputedUserset: &openfga.ObjectRelation{Relation: &role}})
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
	user, err := c.subjectUser(subject)
	if err != nil {
		return openfga.TupleKey{}, err
	}
	return openfga.TupleKey{User: user, Relation: permission, Object: objectType + ":" + resource.Id}, nil
}

func (c *fgaCodec) targetUser(target *proto.RelationshipTarget) (string, error) {
	if subject := target.GetSubject(); subject != nil {
		return c.subjectUser(subject)
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

func (c *fgaCodec) subjectUser(subject *proto.Subject) (string, error) {
	if subject == nil {
		return "", fmt.Errorf("subject is required")
	}
	typeName := c.types[subject.Type]
	if typeName == "" {
		return "", fmt.Errorf("unknown subject type %q", subject.Type)
	}
	return typeName + ":" + subject.Id, nil
}

func (c *fgaCodec) readFilter(filter *proto.RelationshipFilter) (*openfga.ReadRequestTupleKey, error) {
	if filter == nil {
		return nil, nil
	}
	key := &openfga.ReadRequestTupleKey{}
	if filter.Resource != nil {
		if objectType := c.types[filter.Resource.Type]; objectType != "" {
			value := objectType + ":" + filter.Resource.Id
			key.Object = &value
		}
	}
	if filter.Target != nil {
		value, err := c.targetUser(filter.Target)
		if err != nil {
			return nil, err
		}
		key.User = &value
	}
	// OpenFGA's Read API only accepts exact object IDs, not a type prefix.
	// Leave resource-type-only filtering unbounded and apply it locally after
	// reading the tuples.
	if filter.Relation != "" {
		if relation := c.relationsForFilter(filter); relation != "" {
			key.Relation = &relation
		}
	}
	if !key.HasObject() && !key.HasRelation() && !key.HasUser() {
		return nil, nil
	}
	return key, nil
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
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure the compatibility implementation keeps the existing provider contract.
var _ core.AuthorizationProvider = (*openFGA)(nil)
