package agent

type ResolveToolsRequest struct {
	ToolRefs         []ToolRef
	ToolSource       ToolSourceMode
	CallerAppName string
}

type CreateSessionRequest struct {
	SessionID         string
	IdempotencyKey    string
	Model             string
	ClientRef         string
	Metadata          map[string]any
	CreatedBy         Actor
	Subject           SubjectContext
	SessionStart      *SessionStartConfig
	Workspace         *Workspace
	PreparedWorkspace *PreparedWorkspace
}

type GetSessionRequest struct {
	SessionID string
	Subject   SubjectContext
}

type ListSessionsRequest struct {
	Subject    SubjectContext
	SessionIDs []string
	State      SessionState
	// Limit asks providers to apply the cap before returning.
	// Providers must order sessions by recency before limiting: last turn time, then
	// updated time, then created time, newest first.
	Limit int
	// SummaryOnly asks providers to omit heavy fields.
	// Exact SessionIDs may still be served by direct lookup rather than projections.
	SummaryOnly bool
}

type UpdateSessionRequest struct {
	SessionID string
	ClientRef string
	State     SessionState
	Metadata  map[string]any
	Subject   SubjectContext
}

type CreateTurnRequest struct {
	TurnID            string
	SessionID         string
	IdempotencyKey    string
	Model             string
	Messages          []Message
	ToolRefs          []ToolRef
	ToolSource        ToolSourceMode
	Tools             []Tool
	ResponseSchema    map[string]any
	ResponseSchemaSet bool
	Metadata          map[string]any
	ModelOptions      map[string]any
	TimeoutSeconds    int
	CreatedBy         Actor
	ExecutionRef      string
	Subject           SubjectContext
	RunGrant          string
}

type GetTurnRequest struct {
	TurnID  string
	Subject SubjectContext
}

type ListTurnsRequest struct {
	SessionID string
	Subject   SubjectContext
	TurnIDs   []string
	Status    ExecutionStatus
	// Limit asks providers to apply the cap before returning.
	// Providers must order turns by creation time, newest first, before limiting.
	Limit int
	// SummaryOnly asks providers to omit heavy turn fields.
	// Exact TurnIDs may still be served by direct lookup rather than projections.
	SummaryOnly bool
}

type CancelTurnRequest struct {
	TurnID  string
	Reason  string
	Subject SubjectContext
}

type ListTurnEventsRequest struct {
	TurnID   string
	AfterSeq int64
	Limit    int
	Subject  SubjectContext
}

type GetCapabilitiesRequest struct{}

type GetInteractionRequest struct {
	InteractionID string
	Subject       SubjectContext
}

type ListInteractionsRequest struct {
	TurnID  string
	Subject SubjectContext
}

type ResolveInteractionRequest struct {
	InteractionID string
	Resolution    map[string]any
	Subject       SubjectContext
}

type ExecuteToolRequest struct {
	ProviderName   string
	SessionID      string
	TurnID         string
	ToolCallID     string
	ToolID         string
	Arguments      map[string]any
	RunGrant       string
	IdempotencyKey string
}

type ExecuteToolResponse struct {
	Status int
	Body   string
}

type ListToolsRequest struct {
	ProviderName string
	SessionID    string
	TurnID       string
	PageSize     int
	PageToken    string
	Query        string
	ToolRefs     []ToolRef
	ToolSource   ToolSourceMode
	RunGrant     string
}

type ListToolsResponse struct {
	Tools         []ListedTool
	NextPageToken string
}

type ResolveConnectionRequest struct {
	ProviderName string
	SessionID    string
	TurnID       string
	Connection   string
	Instance     string
	RunGrant     string
}

type ManagerCreateSessionRequest struct {
	IdempotencyKey string
	ProviderName   string
	Model          string
	ClientRef      string
	Metadata       map[string]any
	Workspace      *Workspace
}

type ManagerUpdateSessionRequest struct {
	SessionID string
	ClientRef string
	State     SessionState
	Metadata  map[string]any
}

type ManagerCreateTurnRequest struct {
	CallerAppName     string
	IdempotencyKey    string
	Model             string
	SessionID         string
	Messages          []Message
	ToolRefs          []ToolRef
	ToolRefsSet       bool
	ToolSource        ToolSourceMode
	ResponseSchema    map[string]any
	ResponseSchemaSet bool
	Metadata          map[string]any
	ModelOptions      map[string]any
	TimeoutSeconds    int
}

type ManagerListSessionsRequest struct {
	ProviderName string
	State        SessionState
	// Limit caps the globally sorted manager response. Bounded providers receive
	// this cap for per-provider listing.
	Limit       int
	SummaryOnly bool
}

type ManagerListTurnsRequest struct {
	SessionID   string
	Status      ExecutionStatus
	Limit       int
	SummaryOnly bool
}

type ManagerCancelTurnRequest struct {
	TurnID string
	Reason string
}
