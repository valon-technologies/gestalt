package appaccess

import (
	"context"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type staticWorkflowRunResolver struct {
	run *coreworkflow.Run
	err error
}

func (r staticWorkflowRunResolver) ResolveWorkflowRun(context.Context, string, string) (*coreworkflow.Run, error) {
	return r.run, r.err
}

type recordingAppInvocation struct {
	idempotencyKey        string
	internalConnection    bool
	workflowContext       map[string]any
	subjectID             string
	credentialSubjectID   string
	agentSubjectID        string
	runAsSubjectID        string
	providerName          string
	instance              string
	operation             string
	credentialMode        core.ConnectionMode
	tokenPermissions      principal.PermissionSet
	params                map[string]any
	graphQLIdempotencyKey string
	graphQLProviderName   string
	graphQLInstance       string
	graphQLDocument       string
	graphQLVariables      map[string]any
}

func (i *recordingAppInvocation) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	runAsAudit := invocation.RunAsAuditFromContext(ctx)
	i.idempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
	i.internalConnection = invocation.InternalConnectionAccessFromContext(ctx)
	i.workflowContext = invocation.WorkflowContextFromContext(ctx)
	if p != nil {
		i.subjectID = p.SubjectID
		i.credentialSubjectID = p.CredentialSubjectID
		i.tokenPermissions = principal.ClonePermissionSet(p.TokenPermissions)
	}
	if runAsAudit.AgentSubject != nil {
		i.agentSubjectID = runAsAudit.AgentSubject.SubjectID
	}
	if runAsAudit.RunAsSubject != nil {
		i.runAsSubjectID = runAsAudit.RunAsSubject.SubjectID
	}
	i.providerName = providerName
	i.instance = instance
	i.operation = operation
	i.credentialMode = invocation.CredentialModeOverrideFromContext(ctx)
	i.params = params
	return &core.OperationResult{Status: 202, Body: "accepted"}, nil
}

func TestAppServerInvokeRestoresWorkflowContext(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-workflow-context-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "service_account:workflow",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	})
	ctx = invocation.WithWorkflowContext(ctx, map[string]any{
		"providerName": "temporal",
		"runId":        "run-123",
		"step": map[string]any{
			"id": "notify",
		},
	})
	rootToken, err := tokens.MintRootToken(ctx, "workflow-provider", InvocationGrants{
		"slack": {Operations: map[string]core.ConnectionMode{"chat.postMessage": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	invoker := &recordingAppInvocation{}
	server := NewAppServer(
		invoker,
		tokens,
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	if _, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             "slack",
		Operation:       "chat.postMessage",
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if got := invocation.WorkflowContextString(invoker.workflowContext, "providerName"); got != "temporal" {
		t.Fatalf("providerName = %q, want temporal", got)
	}
	if got := invocation.WorkflowContextString(invoker.workflowContext, "runId"); got != "run-123" {
		t.Fatalf("runId = %q, want run-123", got)
	}
	step := invocation.WorkflowContextMap(invoker.workflowContext, "step")
	if got := invocation.WorkflowContextString(step, "id"); got != "notify" {
		t.Fatalf("step.id = %q, want notify", got)
	}
}

func TestAppServerInvokeUsesRequestWorkflowContext(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-request-workflow-context-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "service_account:workflow",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	})
	ctx = invocation.WithWorkflowContext(ctx, map[string]any{
		"providerName": "temporal",
		"runId":        "run-from-token",
	})
	rootToken, err := tokens.MintRootToken(ctx, "workflow-provider", InvocationGrants{
		"slack": {Operations: map[string]core.ConnectionMode{"chat.postMessage": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}
	workflow, err := structpb.NewStruct(map[string]any{
		"providerName": "indexeddb",
		"runId":        "run-from-request",
		"step": map[string]any{
			"id": "notify",
		},
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}

	invoker := &recordingAppInvocation{}
	server := NewAppServer(
		invoker,
		tokens,
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	if _, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             "slack",
		Operation:       "chat.postMessage",
		Workflow:        workflow,
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if got := invocation.WorkflowContextString(invoker.workflowContext, "providerName"); got != "indexeddb" {
		t.Fatalf("providerName = %q, want indexeddb", got)
	}
	if got := invocation.WorkflowContextString(invoker.workflowContext, "runId"); got != "run-from-request" {
		t.Fatalf("runId = %q, want run-from-request", got)
	}
	step := invocation.WorkflowContextMap(invoker.workflowContext, "step")
	if got := invocation.WorkflowContextString(step, "id"); got != "notify" {
		t.Fatalf("step.id = %q, want notify", got)
	}
}

func TestAppServerInvokeUsesWorkflowRunAsWithoutInvocationToken(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-workflow-runas-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	workflow, err := structpb.NewStruct(map[string]any{
		"providerName": "indexeddb",
		"runId":        "run-123",
		"step": map[string]any{
			"id": "review",
		},
		"runAs": map[string]any{
			"id":   "user:forged",
			"kind": "user",
		},
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}

	invoker := &recordingAppInvocation{}
	server := NewAppServer(invoker, tokens, WithWorkflowRunResolver(staticWorkflowRunResolver{
		run: &coreworkflow.Run{
			ID: "run-123",
			Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
				ID: "review",
				App: &coreworkflow.AppCall{
					Name:      "codeReview",
					Operation: "pullRequests.reviewWorkflow",
				},
			}}},
			RunAs: &core.RunAsSubject{
				SubjectID:           "user:workflow-runner",
				SubjectKind:         "user",
				CredentialSubjectID: "user:workflow-runner",
				DisplayName:         "Workflow runner",
				AuthSource:          "config",
			},
		},
	}))
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	if _, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "codeReview",
		Operation: "pullRequests.reviewWorkflow",
		Workflow:  workflow,
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if invoker.subjectID != "user:workflow-runner" {
		t.Fatalf("subject id = %q, want user:workflow-runner", invoker.subjectID)
	}
	if invoker.credentialSubjectID != "user:workflow-runner" {
		t.Fatalf("credential subject id = %q, want user:workflow-runner", invoker.credentialSubjectID)
	}
	if invoker.providerName != "codeReview" {
		t.Fatalf("provider name = %q, want codeReview", invoker.providerName)
	}
	if invoker.operation != "pullRequests.reviewWorkflow" {
		t.Fatalf("operation = %q, want pullRequests.reviewWorkflow", invoker.operation)
	}
	if got := invocation.WorkflowContextString(invoker.workflowContext, "providerName"); got != "indexeddb" {
		t.Fatalf("providerName = %q, want indexeddb", got)
	}
	if got := invocation.WorkflowContextString(invoker.workflowContext, "runId"); got != "run-123" {
		t.Fatalf("runId = %q, want run-123", got)
	}
	step := invocation.WorkflowContextMap(invoker.workflowContext, "step")
	if got := invocation.WorkflowContextString(step, "id"); got != "review" {
		t.Fatalf("step.id = %q, want review", got)
	}
	runAs := invocation.WorkflowContextMap(invoker.workflowContext, "runAs")
	if got := invocation.WorkflowContextString(runAs, "id"); got != "user:workflow-runner" {
		t.Fatalf("workflow runAs id = %q, want persisted runAs", got)
	}
}

func TestAppServerInvokeWorkflowRunAsInheritsDirectStepAppInvokes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		stepApp     string
		wantCode    codes.Code
		wantInvoked bool
	}{
		{
			name:        "direct step app invoke",
			stepApp:     "worker",
			wantInvoked: true,
		},
		{
			name:     "unrelated app invoke",
			stepApp:  "other",
			wantCode: codes.PermissionDenied,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-workflow-step-invokes-" + tc.name))
			if err != nil {
				t.Fatalf("NewInvocationTokenManager: %v", err)
			}
			workflow, err := structpb.NewStruct(map[string]any{
				"providerName": "workflow-provider",
				"runId":        "run-123",
			})
			if err != nil {
				t.Fatalf("NewStruct: %v", err)
			}

			invoker := &recordingAppInvocation{}
			server := NewAppServer(invoker, tokens,
				WithWorkflowRunResolver(staticWorkflowRunResolver{
					run: &coreworkflow.Run{
						ID: "run-123",
						Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
							ID: "run",
							App: &coreworkflow.AppCall{
								Name:      tc.stepApp,
								Operation: "run",
							},
						}}},
						RunAs: &core.RunAsSubject{SubjectID: "service_account:workflow-runner"},
					},
				}),
				WithWorkflowAppInvocationGrants(map[string]InvocationGrants{
					"worker": ExactInvocationGrantsFromDependencies([]InvocationDependency{{
						App:            "downstream",
						Operation:      "read",
						CredentialMode: core.ConnectionModeNone,
					}}),
				}),
			)
			client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
				proto.RegisterAppServer(srv, server)
			}))

			_, err = client.Invoke(context.Background(), &proto.AppInvokeRequest{
				App:       "downstream",
				Operation: "read",
				Workflow:  workflow,
			})
			if tc.wantCode != codes.OK {
				if got := status.Code(err); got != tc.wantCode {
					t.Fatalf("Invoke status = %s, want %s (err=%v)", got, tc.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if !tc.wantInvoked {
				t.Fatal("test case succeeded unexpectedly")
			}
			if invoker.providerName != "downstream" || invoker.operation != "read" {
				t.Fatalf("target = %s.%s, want downstream.read", invoker.providerName, invoker.operation)
			}
			if invoker.credentialMode != core.ConnectionModeNone {
				t.Fatalf("credential mode = %q, want none", invoker.credentialMode)
			}
			if invoker.subjectID != "service_account:workflow-runner" {
				t.Fatalf("subject id = %q, want persisted runAs subject", invoker.subjectID)
			}
			if _, ok := invoker.tokenPermissions["downstream"]["read"]; !ok {
				t.Fatalf("token permissions = %#v, want downstream.read", invoker.tokenPermissions)
			}
		})
	}
}

func TestAppServerInvokeRequiresPersistedWorkflowRunAsWithoutInvocationToken(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		workflow map[string]any
		run      *coreworkflow.Run
		wantCode codes.Code
	}{
		{
			name:     "missing workflow",
			wantCode: codes.FailedPrecondition,
		},
		{
			name:     "missing provider",
			workflow: map[string]any{"runId": "run-123"},
			wantCode: codes.FailedPrecondition,
		},
		{
			name:     "missing run id",
			workflow: map[string]any{"providerName": "indexeddb"},
			wantCode: codes.FailedPrecondition,
		},
		{
			name:     "missing persisted runAs",
			workflow: map[string]any{"providerName": "indexeddb", "runId": "run-123"},
			run: &coreworkflow.Run{
				ID:     "run-123",
				Target: coreworkflow.Target{Steps: []coreworkflow.Step{{ID: "review", App: &coreworkflow.AppCall{Name: "codeReview", Operation: "pullRequests.reviewWorkflow"}}}},
			},
			wantCode: codes.FailedPrecondition,
		},
		{
			name:     "operation outside persisted target",
			workflow: map[string]any{"providerName": "indexeddb", "runId": "run-123"},
			run: &coreworkflow.Run{
				ID:     "run-123",
				Target: coreworkflow.Target{Steps: []coreworkflow.Step{{ID: "other", App: &coreworkflow.AppCall{Name: "codeReview", Operation: "pullRequests.other"}}}},
				RunAs:  &core.RunAsSubject{SubjectID: "user:workflow-runner"},
			},
			wantCode: codes.PermissionDenied,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-invalid-workflow-runas-" + tc.name))
			if err != nil {
				t.Fatalf("NewInvocationTokenManager: %v", err)
			}
			var workflow *structpb.Struct
			if tc.workflow != nil {
				var err error
				workflow, err = structpb.NewStruct(tc.workflow)
				if err != nil {
					t.Fatalf("NewStruct: %v", err)
				}
			}

			server := NewAppServer(&recordingAppInvocation{}, tokens, WithWorkflowRunResolver(staticWorkflowRunResolver{run: tc.run}))
			client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
				proto.RegisterAppServer(srv, server)
			}))
			_, err = client.Invoke(context.Background(), &proto.AppInvokeRequest{
				App:       "codeReview",
				Operation: "pullRequests.reviewWorkflow",
				Workflow:  workflow,
			})
			if got := status.Code(err); got != tc.wantCode {
				t.Fatalf("Invoke status = %s, want %s (err=%v)", got, tc.wantCode, err)
			}
		})
	}
}

func TestAppServerInvokeCredentialModeForForwardedToken(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		grantMode   core.ConnectionMode
		requestMode string
		want        core.ConnectionMode
		wantErr     codes.Code
	}{
		{
			name:        "explicit token mode",
			grantMode:   core.ConnectionModeNone,
			requestMode: "none",
			want:        core.ConnectionModeNone,
		},
		{
			name:        "explicit unqualified mode",
			requestMode: "none",
			want:        core.ConnectionModeNone,
		},
		{
			name:        "explicit subject mode",
			requestMode: "subject",
			want:        core.ConnectionModeSubject,
		},
		{
			name:        "unsupported mode rejected",
			requestMode: "unsupported-mode",
			wantErr:     codes.InvalidArgument,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := NewInvocationTokenManager([]byte("forwarded-token-" + tc.name))
			if err != nil {
				t.Fatalf("NewInvocationTokenManager: %v", err)
			}
			ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
				SubjectID: "service_account:caller",
				Kind:      principal.Kind("service_account"),
				Source:    principal.SourceAPIToken,
			})
			rootToken, err := tokens.MintRootToken(ctx, "caller", InvocationGrants{
				"slack": {Operations: map[string]core.ConnectionMode{"chat.postMessage": tc.grantMode}},
			})
			if err != nil {
				t.Fatalf("MintRootToken: %v", err)
			}

			invoker := &recordingAppInvocation{}
			server := NewAppServer(invoker, tokens)
			client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
				proto.RegisterAppServer(srv, server)
			}))
			_, err = client.Invoke(context.Background(), &proto.AppInvokeRequest{
				InvocationToken: rootToken,
				App:             "slack",
				Operation:       "chat.postMessage",
				CredentialMode:  tc.requestMode,
			})
			if tc.wantErr != codes.OK {
				if got := status.Code(err); got != tc.wantErr {
					t.Fatalf("Invoke status = %s, want %s (err=%v)", got, tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if invoker.credentialMode != tc.want {
				t.Fatalf("credential mode override = %q, want %q", invoker.credentialMode, tc.want)
			}
		})
	}
}

func TestAppServerInvokeAppliesConfiguredDelegationMetadata(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-delegation-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID:           "user:test-user",
		CredentialSubjectID: "user:test-user",
		DisplayName:         "Test User",
		Kind:                principal.KindUser,
		Source:              principal.SourceSession,
	})
	rootToken, err := tokens.MintRootToken(ctx, "data-schema-explorer", InvocationGrants{
		"frontPorchRestApi": {
			AllOperations: true,
			Operations: map[string]core.ConnectionMode{
				"vds.schemaVersions": core.ConnectionModeSubject,
			},
			OperationDelegations: map[string]InvocationDelegation{
				"vds.schemaVersions": {
					RunAs: &core.RunAsSubject{
						SubjectID:           "service_account:data-schema-explorer",
						SubjectKind:         "service_account",
						CredentialSubjectID: "service_account:data-schema-explorer",
						DisplayName:         "Data Schema Explorer",
						AuthSource:          "app_invocation",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}
	params, err := structpb.NewStruct(map[string]any{
		"owner": "valon-technologies",
		"repo":  "toolshed",
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}

	invoker := &recordingAppInvocation{}
	server := NewAppServer(invoker, tokens)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	if _, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             "frontPorchRestApi",
		Operation:       "vds.schemaVersions",
		Params:          params,
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if invoker.subjectID != "service_account:data-schema-explorer" {
		t.Fatalf("subject = %q, want service_account:data-schema-explorer", invoker.subjectID)
	}
	if invoker.credentialSubjectID != "service_account:data-schema-explorer" {
		t.Fatalf("credential subject = %q, want service_account:data-schema-explorer", invoker.credentialSubjectID)
	}
	if invoker.agentSubjectID != "user:test-user" {
		t.Fatalf("run-as audit agent subject = %q, want subject:test-user", invoker.agentSubjectID)
	}
	if invoker.runAsSubjectID != "service_account:data-schema-explorer" {
		t.Fatalf("run-as audit subject = %q, want service_account:data-schema-explorer", invoker.runAsSubjectID)
	}
	if invoker.credentialMode != core.ConnectionModeSubject {
		t.Fatalf("credential mode override = %q, want subject", invoker.credentialMode)
	}
}

func TestAppServerInvokePropagatesInternalConnectionAccess(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "service_account:workflow-config",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	})
	ctx = invocation.WithInternalConnectionAccess(ctx)
	rootToken, err := tokens.MintRootToken(ctx, "brain", InvocationGrants{
		"slack": {Operations: map[string]core.ConnectionMode{"conversations.history": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	invoker := &recordingAppInvocation{}
	server := NewAppServer(
		invoker,
		tokens,
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	if _, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             "slack",
		Operation:       "conversations.history",
		Connection:      "bot",
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !invoker.internalConnection {
		t.Fatal("internal connection access was not restored from the invocation token")
	}
}

func TestAppServerInvokeMapsInvalidInvocationToInvalidArgument(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-invalid-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "service_account:workflow-config",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	})
	rootToken, err := tokens.MintRootToken(ctx, "brain", InvocationGrants{
		"gmail": {Operations: map[string]core.ConnectionMode{"gmail.users.messages.modify": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	server := NewAppServer(
		erroringAppInvocation{err: fmt.Errorf("%w: bad connection override", invocation.ErrInvalidInvocation)},
		tokens,
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	_, err = client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             "gmail",
		Operation:       "gmail.users.messages.modify",
		Connection:      "override",
	})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("Invoke status = %s, want %s (err=%v)", got, codes.InvalidArgument, err)
	}
}

func (i *recordingAppInvocation) InvokeGraphQL(ctx context.Context, _ *principal.Principal, providerName, instance string, request invocation.GraphQLRequest) (*core.OperationResult, error) {
	i.graphQLIdempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
	i.graphQLProviderName = providerName
	i.graphQLInstance = instance
	i.graphQLDocument = request.Document
	i.graphQLVariables = request.Variables
	return &core.OperationResult{Status: 208, Body: "graphql-accepted"}, nil
}

type erroringAppInvocation struct {
	err error
}

func (i erroringAppInvocation) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, i.err
}

func TestAppServerInvokePropagatesIdempotencyKey(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "user:test-user",
		UserID:    "test-user",
		Kind:      principal.KindUser,
		Source:    principal.SourceSession,
	})
	rootToken, err := tokens.MintRootToken(ctx, "caller", InvocationGrants{
		"github": {Operations: map[string]core.ConnectionMode{"issues.create": ""}},
		"linear": {Surfaces: map[string]struct{}{"graphql": {}}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	invoker := &recordingAppInvocation{}
	server := NewAppServer(
		invoker,
		tokens,
	)
	params, err := structpb.NewStruct(map[string]any{"title": "bug"})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	resp, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             " github ",
		Operation:       " issues.create ",
		Instance:        " prod ",
		IdempotencyKey:  " tool-call-123 ",
		Params:          params,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetStatus() != 202 || resp.GetBody() != "accepted" {
		t.Fatalf("Invoke response = %+v, want status=202 body=accepted", resp)
	}
	if invoker.idempotencyKey != "tool-call-123" {
		t.Fatalf("idempotency key = %q, want tool-call-123", invoker.idempotencyKey)
	}
	if invoker.providerName != "github" || invoker.operation != "issues.create" || invoker.instance != "prod" {
		t.Fatalf("target = %s.%s/%s, want github.issues.create/prod", invoker.providerName, invoker.operation, invoker.instance)
	}
	if invoker.params["title"] != "bug" {
		t.Fatalf("params = %#v, want title=bug", invoker.params)
	}

	variables, err := structpb.NewStruct(map[string]any{"team": "eng"})
	if err != nil {
		t.Fatalf("NewStruct variables: %v", err)
	}
	graphQLResp, err := client.InvokeGraphQL(context.Background(), &proto.AppInvokeGraphQLRequest{
		InvocationToken: rootToken,
		App:             " linear ",
		Document:        " query Viewer { viewer { id } } ",
		Instance:        " prod ",
		IdempotencyKey:  " graphql-call-123 ",
		Variables:       variables,
	})
	if err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
	if graphQLResp.GetStatus() != 208 || graphQLResp.GetBody() != "graphql-accepted" {
		t.Fatalf("InvokeGraphQL response = %+v, want status=208 body=graphql-accepted", graphQLResp)
	}
	if invoker.graphQLIdempotencyKey != "graphql-call-123" {
		t.Fatalf("graphql idempotency key = %q, want graphql-call-123", invoker.graphQLIdempotencyKey)
	}
	if invoker.graphQLProviderName != "linear" || invoker.graphQLInstance != "prod" {
		t.Fatalf("graphql target = %s/%s, want linear/prod", invoker.graphQLProviderName, invoker.graphQLInstance)
	}
	if invoker.graphQLDocument != "query Viewer { viewer { id } }" {
		t.Fatalf("graphql document = %q, want trimmed document", invoker.graphQLDocument)
	}
	if invoker.graphQLVariables["team"] != "eng" {
		t.Fatalf("graphql variables = %#v, want team=eng", invoker.graphQLVariables)
	}
}
