package gestalt

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// EnvIndexedDBSocket is the default Unix-socket environment variable used by
// [IndexedDB].
const EnvIndexedDBSocket = "GESTALT_INDEXEDDB_SOCKET"
const indexedDBSocketTokenSuffix = "_TOKEN"

// IndexedDBSocketEnv returns the environment variable name used for a named
// IndexedDB transport socket.
func IndexedDBSocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return EnvIndexedDBSocket
	}
	var b strings.Builder
	b.WriteString(EnvIndexedDBSocket)
	b.WriteByte('_')
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// IndexedDBSocketTokenEnv returns the companion environment variable name used
// to discover a host-service relay token for an IndexedDB binding.
func IndexedDBSocketTokenEnv(name string) string {
	return IndexedDBSocketEnv(name) + indexedDBSocketTokenSuffix
}

var (
	// ErrNotFound indicates that the requested store entry or cursor row does
	// not exist.
	ErrNotFound = fmt.Errorf("indexeddb: not found")
	// ErrAlreadyExists indicates that a record or object store already exists.
	ErrAlreadyExists = fmt.Errorf("indexeddb: already exists")
	// ErrAbort indicates that an IndexedDB lifecycle operation was aborted.
	ErrAbort = fmt.Errorf("indexeddb: operation aborted")
	// ErrBlocked indicates that an IndexedDB lifecycle operation is blocked by
	// open connections or active operations.
	ErrBlocked = fmt.Errorf("indexeddb: operation blocked")
	// ErrKeysOnly indicates that the current cursor was opened in key-only mode
	// and therefore has no value payload.
	ErrKeysOnly = fmt.Errorf("indexeddb: value not available on key-only cursor")
	// ErrTransactionDone indicates that a transaction has already committed,
	// aborted, or failed.
	ErrTransactionDone = fmt.Errorf("indexeddb: transaction is already finished")
	// ErrReadOnly indicates that a readonly transaction received a write request.
	ErrReadOnly = fmt.Errorf("indexeddb: transaction is readonly")
	// ErrInvalidTransaction indicates an invalid transaction scope or mode.
	ErrInvalidTransaction = fmt.Errorf("indexeddb: invalid transaction")
)

// CursorDirection controls IndexedDB cursor traversal order.
type CursorDirection string

const (
	// CursorNext iterates forward and emits duplicate index keys.
	CursorNext CursorDirection = "next"
	// CursorNextUnique iterates forward while collapsing duplicate index keys.
	CursorNextUnique CursorDirection = "nextunique"
	// CursorPrev iterates backward and emits duplicate index keys.
	CursorPrev CursorDirection = "prev"
	// CursorPrevUnique iterates backward while collapsing duplicate index keys.
	CursorPrevUnique CursorDirection = "prevunique"
)

// Record is the JSON-like value stored in an object store row.
type Record = map[string]any

// TransactionMode controls whether a transaction may mutate scoped stores.
type TransactionMode string

const (
	TransactionReadonly  TransactionMode = "readonly"
	TransactionReadwrite TransactionMode = "readwrite"
)

// TransactionDurabilityHint mirrors the W3C IndexedDB durability option as a
// provider hint. It is not a portable durability guarantee.
type TransactionDurabilityHint string

const (
	TransactionDurabilityDefault TransactionDurabilityHint = "default"
	TransactionDurabilityStrict  TransactionDurabilityHint = "strict"
	TransactionDurabilityRelaxed TransactionDurabilityHint = "relaxed"
)

type TransactionOptions struct {
	DurabilityHint TransactionDurabilityHint
}

// IDBDatabaseInfo describes one database returned from Databases.
type IDBDatabaseInfo struct {
	Name    string
	Version uint64
}

type VersionChangeReason string

const (
	VersionChangeUpgrade VersionChangeReason = "upgrade"
	VersionChangeDelete  VersionChangeReason = "delete"
)

type VersionChangeInfo struct {
	Name       string
	OldVersion uint64
	NewVersion *uint64
	Reason     VersionChangeReason
}

type BlockedInfo struct {
	Name               string
	OldVersion         uint64
	NewVersion         *uint64
	Reason             VersionChangeReason
	OpenConnections    int
	ActiveTransactions int
}

type BlockedAction int

const (
	BlockedFail BlockedAction = iota
	BlockedWait
)

type OpenOptions struct {
	Version         *uint64
	Upgrade         func(context.Context, UpgradeContext) error
	OnVersionChange func(context.Context, VersionChangeInfo) error
	OnBlocked       func(context.Context, BlockedInfo) (BlockedAction, error)
}

type DeleteOptions struct {
	OnBlocked func(context.Context, BlockedInfo) (BlockedAction, error)
}

type DeleteDatabaseResult struct {
	Name       string
	OldVersion uint64
}

type UpgradeContext interface {
	OldVersion() uint64
	NewVersion() uint64
	Database() UpgradeDatabase
	ObjectStoreNames(ctx context.Context) ([]string, error)
	CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error
	DeleteObjectStore(ctx context.Context, name string) error
	CreateIndex(ctx context.Context, store string, index IndexSchema) error
	DeleteIndex(ctx context.Context, store string, name string) error
}

type UpgradeDatabase interface {
	Name() string
	ObjectStoreNames(ctx context.Context) ([]string, error)
	CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error
	DeleteObjectStore(ctx context.Context, name string) error
	CreateIndex(ctx context.Context, store string, index IndexSchema) error
	DeleteIndex(ctx context.Context, store string, name string) error
}

// KeyRange constrains range queries and cursors by lower and upper bounds.
type KeyRange struct {
	Lower     any
	Upper     any
	LowerOpen bool
	UpperOpen bool
}

// IndexSchema describes one secondary index on an object store.
type IndexSchema struct {
	Name    string
	KeyPath []string
	Unique  bool
}

// ColumnType describes a provider-preserved scalar column type.
type ColumnType int32

const (
	// TypeString stores UTF-8 string values.
	TypeString ColumnType = iota
	// TypeInt stores 64-bit signed integer values.
	TypeInt
	// TypeFloat stores IEEE-754 double values.
	TypeFloat
	// TypeBool stores boolean values.
	TypeBool
	// TypeTime stores timestamp values.
	TypeTime
	// TypeBytes stores binary blob values.
	TypeBytes
	// TypeJSON stores JSON-like structured values.
	TypeJSON
)

// ColumnDef describes one provider-preserved object-store column.
type ColumnDef struct {
	Name       string
	Type       ColumnType
	PrimaryKey bool
	NotNull    bool
	Unique     bool
}

// ObjectStoreSchema describes the indexes and columns attached to an object
// store.
type ObjectStoreSchema struct {
	Indexes []IndexSchema
	Columns []ColumnDef
}

// IndexedDBClient speaks to a running IndexedDB provider over a host-provided
// transport target.
type IndexedDBClient struct {
	client proto.IndexedDBClient
	conn   *grpc.ClientConn
}

// IndexedDB connects to the IndexedDB provider exposed by gestaltd. The target
// can be a plain Unix socket path, a unix:///path URI, or a tcp://host:port or
// tls://host:port URI.
func IndexedDB(name ...string) (*IndexedDBClient, error) {
	envName := EnvIndexedDBSocket
	if len(name) > 0 {
		envName = IndexedDBSocketEnv(name[0])
	}
	target := os.Getenv(envName)
	if target == "" {
		return nil, fmt.Errorf("indexeddb: %s is not set", envName)
	}
	network, address, err := parseIndexedDBTarget(target)
	if err != nil {
		return nil, err
	}
	token := os.Getenv(IndexedDBSocketTokenEnv(firstIndex(name)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var conn *grpc.ClientConn
	opts := indexedDBDialOptions(token)
	switch network {
	case "unix":
		conn, err = grpc.DialContext(ctx, "passthrough:///localhost",
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", address)
				}),
				grpc.WithAuthority("localhost"),
				grpc.WithBlock(),
			), opts...)...,
		)
	case "tcp":
		conn, err = grpc.DialContext(ctx, address,
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithBlock(),
			), opts...)...,
		)
	case "tls":
		host, _, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			return nil, fmt.Errorf("indexeddb: parse tls target %q: %w", address, splitErr)
		}
		tlsConfig, tlsErr := hostServiceTLSConfig("indexeddb", host)
		if tlsErr != nil {
			return nil, tlsErr
		}
		conn, err = grpc.DialContext(ctx, address,
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
				grpc.WithBlock(),
			), opts...)...,
		)
	default:
		return nil, fmt.Errorf("indexeddb: unsupported transport network %q", network)
	}
	if err != nil {
		return nil, fmt.Errorf("indexeddb: connect to host: %w", err)
	}
	return &IndexedDBClient{
		client: proto.NewIndexedDBClient(conn),
		conn:   conn,
	}, nil
}

func indexedDBDialOptions(token string) []grpc.DialOption {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return []grpc.DialOption{grpc.WithPerRPCCredentials(indexedDBRelayPerRPCCredentials{token: token})}
}

func firstIndex(name []string) string {
	if len(name) == 0 {
		return ""
	}
	return name[0]
}

type indexedDBRelayPerRPCCredentials struct {
	token string
}

func (c indexedDBRelayPerRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{
		"x-gestalt-host-service-relay-token": c.token,
	}, nil
}

func (indexedDBRelayPerRPCCredentials) RequireTransportSecurity() bool { return false }

func parseIndexedDBTarget(raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("indexeddb: transport target is required")
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("indexeddb: tcp target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "tls://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
		if address == "" {
			return "", "", fmt.Errorf("indexeddb: tls target %q is missing host:port", raw)
		}
		return "tls", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("indexeddb: unix target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		parsed, parseErr := url.Parse(target)
		if parseErr != nil {
			return "", "", fmt.Errorf("indexeddb: parse target %q: %w", raw, parseErr)
		}
		return "", "", fmt.Errorf("indexeddb: unsupported target scheme %q", parsed.Scheme)
	default:
		return "unix", filepath.Clean(target), nil
	}
}

// Close closes the underlying gRPC transport.
func (db *IndexedDBClient) Close() error {
	return db.conn.Close()
}

// Open opens a database connection, optionally running an upgrade callback when
// a higher version is requested or the database needs to be created.
func (db *IndexedDBClient) Open(ctx context.Context, name string, opts OpenOptions) (*IDBDatabaseClient, error) {
	return db.openDatabase(ctx, &proto.OpenDatabaseRequest{Name: name, Version: opts.Version}, opts)
}

// OpenCurrent opens an existing database at its current version. It fails if
// the database does not exist.
func (db *IndexedDBClient) OpenCurrent(ctx context.Context, name string, opts OpenOptions) (*IDBDatabaseClient, error) {
	opts.Version = nil
	return db.openDatabase(ctx, &proto.OpenDatabaseRequest{Name: name, RequireExisting: true}, opts)
}

func (db *IndexedDBClient) openDatabase(ctx context.Context, req *proto.OpenDatabaseRequest, opts OpenOptions) (*IDBDatabaseClient, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := db.client.OpenDatabase(streamCtx)
	if err != nil {
		cancel()
		return nil, grpcErr(err)
	}
	if err := stream.Send(&proto.OpenDatabaseClientMessage{Msg: &proto.OpenDatabaseClientMessage_Open{Open: req}}); err != nil {
		_ = stream.CloseSend()
		cancel()
		return nil, grpcErr(err)
	}
	for {
		msg, err := stream.Recv()
		if err != nil {
			_ = stream.CloseSend()
			cancel()
			return nil, grpcErr(err)
		}
		switch body := msg.GetMsg().(type) {
		case *proto.OpenDatabaseServerMessage_UpgradeStarted:
			upgrade := &remoteUpgradeContext{stream: stream, started: body.UpgradeStarted}
			if opts.Upgrade == nil {
				if err := upgrade.finish(ctx); err != nil {
					_ = stream.CloseSend()
					cancel()
					return nil, err
				}
				continue
			}
			if err := opts.Upgrade(ctx, upgrade); err != nil {
				abortErr := upgrade.abort(ctx, err.Error())
				_ = stream.CloseSend()
				cancel()
				if abortErr != nil && !errors.Is(abortErr, ErrAbort) {
					return nil, errors.Join(err, abortErr)
				}
				return nil, err
			}
			if !upgrade.finished {
				if err := upgrade.finish(ctx); err != nil {
					_ = stream.CloseSend()
					cancel()
					return nil, err
				}
			}
		case *proto.OpenDatabaseServerMessage_Blocked:
			action := BlockedWait
			if opts.OnBlocked != nil {
				var callbackErr error
				action, callbackErr = opts.OnBlocked(ctx, blockedInfoFromProto(body.Blocked))
				if callbackErr != nil {
					_ = stream.CloseSend()
					cancel()
					return nil, fmt.Errorf("%w: %v", ErrAbort, callbackErr)
				}
			}
			if action == BlockedFail {
				_ = stream.CloseSend()
				cancel()
				return nil, ErrBlocked
			}
		case *proto.OpenDatabaseServerMessage_Opened:
			opened := &IDBDatabaseClient{
				client:           db.client,
				stream:           stream,
				cancel:           cancel,
				connectionID:     append([]byte(nil), body.Opened.GetConnectionId()...),
				name:             body.Opened.GetName(),
				version:          body.Opened.GetVersion(),
				objectStoreNames: append([]string(nil), body.Opened.GetObjectStoreNames()...),
				onVersionChange:  opts.OnVersionChange,
				closed:           make(chan struct{}),
			}
			go opened.recvLifecycle()
			return opened, nil
		case *proto.OpenDatabaseServerMessage_Error:
			_ = stream.CloseSend()
			cancel()
			if err := rpcStatusErr(body.Error); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("indexeddb open: error frame missing non-OK status")
		default:
			_ = stream.CloseSend()
			cancel()
			return nil, fmt.Errorf("indexeddb: unexpected open database message")
		}
	}
}

// DeleteDatabase deletes a database and returns the previous version. Deleting
// a missing database succeeds with old version 0.
func (db *IndexedDBClient) DeleteDatabase(ctx context.Context, name string, opts DeleteOptions) (DeleteDatabaseResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := db.client.DeleteDatabase(streamCtx, &proto.DeleteDatabaseRequest{Name: name})
	if err != nil {
		return DeleteDatabaseResult{}, grpcErr(err)
	}
	for {
		msg, err := stream.Recv()
		if err != nil {
			return DeleteDatabaseResult{}, grpcErr(err)
		}
		switch body := msg.GetMsg().(type) {
		case *proto.DeleteDatabaseServerMessage_Blocked:
			action := BlockedWait
			if opts.OnBlocked != nil {
				var callbackErr error
				action, callbackErr = opts.OnBlocked(ctx, blockedInfoFromProto(body.Blocked))
				if callbackErr != nil {
					cancel()
					return DeleteDatabaseResult{}, fmt.Errorf("%w: %v", ErrAbort, callbackErr)
				}
			}
			if action == BlockedFail {
				cancel()
				return DeleteDatabaseResult{}, ErrBlocked
			}
		case *proto.DeleteDatabaseServerMessage_Deleted:
			return DeleteDatabaseResult{Name: body.Deleted.GetName(), OldVersion: body.Deleted.GetOldVersion()}, nil
		case *proto.DeleteDatabaseServerMessage_Error:
			if err := rpcStatusErr(body.Error); err != nil {
				return DeleteDatabaseResult{}, err
			}
			return DeleteDatabaseResult{}, fmt.Errorf("indexeddb delete database: error frame missing non-OK status")
		default:
			return DeleteDatabaseResult{}, fmt.Errorf("indexeddb: unexpected delete database message")
		}
	}
}

// Databases returns visible database names and versions.
func (db *IndexedDBClient) Databases(ctx context.Context) ([]IDBDatabaseInfo, error) {
	resp, err := db.client.Databases(ctx, &proto.DatabasesRequest{})
	if err != nil {
		return nil, grpcErr(err)
	}
	out := make([]IDBDatabaseInfo, len(resp.GetDatabases()))
	for i, info := range resp.GetDatabases() {
		out[i] = IDBDatabaseInfo{Name: info.GetName(), Version: info.GetVersion()}
	}
	return out, nil
}

// CompareKeys compares two IndexedDB keys using provider key semantics.
func (db *IndexedDBClient) CompareKeys(ctx context.Context, first any, second any) (int, error) {
	firstKey, err := anyToKeyValue(first)
	if err != nil {
		return 0, err
	}
	secondKey, err := anyToKeyValue(second)
	if err != nil {
		return 0, err
	}
	resp, err := db.client.CompareKeys(ctx, &proto.CompareKeysRequest{First: firstKey, Second: secondKey})
	if err != nil {
		return 0, grpcErr(err)
	}
	return int(resp.GetCmp()), nil
}

// IDBDatabaseClient is a live database connection returned from Open.
type IDBDatabaseClient struct {
	client           proto.IndexedDBClient
	stream           proto.IndexedDB_OpenDatabaseClient
	cancel           context.CancelFunc
	connectionID     []byte
	name             string
	version          uint64
	objectStoreNames []string
	onVersionChange  func(context.Context, VersionChangeInfo) error
	closeOnce        sync.Once
	closed           chan struct{}
}

func (db *IDBDatabaseClient) Name() string { return db.name }

func (db *IDBDatabaseClient) Version() uint64 { return db.version }

func (db *IDBDatabaseClient) ObjectStoreNames(context.Context) ([]string, error) {
	return append([]string(nil), db.objectStoreNames...), nil
}

func (db *IDBDatabaseClient) ObjectStore(name string) *IDBObjectStoreClient {
	return &IDBObjectStoreClient{client: db.client, connectionID: db.connectionID, store: name}
}

func (db *IDBDatabaseClient) Transaction(ctx context.Context, stores []string, mode TransactionMode, opts TransactionOptions) (*IDBTransactionClient, error) {
	return beginTransaction(ctx, db.client, db.connectionID, stores, mode, opts)
}

func (db *IDBDatabaseClient) Close() error {
	db.closeOnce.Do(func() {
		if db.stream != nil {
			_ = db.stream.Send(&proto.OpenDatabaseClientMessage{Msg: &proto.OpenDatabaseClientMessage_Close{Close: &proto.CloseDatabaseRequest{}}})
			_ = db.stream.CloseSend()
		}
		if db.cancel != nil {
			db.cancel()
		}
		<-db.closed
	})
	return nil
}

func (db *IDBDatabaseClient) recvLifecycle() {
	defer close(db.closed)
	for {
		msg, err := db.stream.Recv()
		if err != nil {
			return
		}
		switch body := msg.GetMsg().(type) {
		case *proto.OpenDatabaseServerMessage_Versionchange:
			if db.onVersionChange != nil {
				info := versionChangeInfoFromProto(body.Versionchange)
				go func() { _ = db.onVersionChange(context.Background(), info) }()
			}
		case *proto.OpenDatabaseServerMessage_Closed, *proto.OpenDatabaseServerMessage_Error:
			return
		}
	}
}

type remoteUpgradeContext struct {
	stream   proto.IndexedDB_OpenDatabaseClient
	started  *proto.UpgradeStarted
	nextID   uint64
	finished bool
}

func (u *remoteUpgradeContext) OldVersion() uint64 { return u.started.GetOldVersion() }

func (u *remoteUpgradeContext) NewVersion() uint64 { return u.started.GetNewVersion() }

func (u *remoteUpgradeContext) Database() UpgradeDatabase {
	return (*remoteUpgradeDatabase)(u)
}

func (u *remoteUpgradeContext) ObjectStoreNames(ctx context.Context) ([]string, error) {
	resp, err := u.send(ctx, &proto.UpgradeOperation{Op: &proto.UpgradeOperation_ObjectStoreNames{ObjectStoreNames: &proto.UpgradeObjectStoreNamesRequest{}}})
	if err != nil {
		return nil, err
	}
	return append([]string(nil), resp.GetObjectStoreNames()...), nil
}

func (u *remoteUpgradeContext) CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error {
	_, err := u.send(ctx, &proto.UpgradeOperation{Op: &proto.UpgradeOperation_CreateObjectStore{CreateObjectStore: &proto.UpgradeCreateObjectStoreRequest{
		Name:   name,
		Schema: objectStoreSchemaToProto(schema),
	}}})
	return err
}

func (u *remoteUpgradeContext) DeleteObjectStore(ctx context.Context, name string) error {
	_, err := u.send(ctx, &proto.UpgradeOperation{Op: &proto.UpgradeOperation_DeleteObjectStore{DeleteObjectStore: &proto.UpgradeDeleteObjectStoreRequest{Name: name}}})
	return err
}

func (u *remoteUpgradeContext) CreateIndex(ctx context.Context, store string, index IndexSchema) error {
	_, err := u.send(ctx, &proto.UpgradeOperation{Op: &proto.UpgradeOperation_CreateIndex{CreateIndex: &proto.UpgradeCreateIndexRequest{
		Store:   store,
		Name:    index.Name,
		KeyPath: index.KeyPath,
		Unique:  index.Unique,
	}}})
	return err
}

func (u *remoteUpgradeContext) DeleteIndex(ctx context.Context, store string, name string) error {
	_, err := u.send(ctx, &proto.UpgradeOperation{Op: &proto.UpgradeOperation_DeleteIndex{DeleteIndex: &proto.UpgradeDeleteIndexRequest{Store: store, Name: name}}})
	return err
}

func (u *remoteUpgradeContext) finish(ctx context.Context) error {
	_, err := u.send(ctx, &proto.UpgradeOperation{Op: &proto.UpgradeOperation_FinishUpgrade{FinishUpgrade: &proto.FinishUpgradeRequest{}}})
	if err == nil {
		u.finished = true
	}
	return err
}

func (u *remoteUpgradeContext) abort(ctx context.Context, reason string) error {
	_, err := u.send(ctx, &proto.UpgradeOperation{Op: &proto.UpgradeOperation_AbortUpgrade{AbortUpgrade: &proto.AbortUpgradeRequest{Reason: reason}}})
	if err == nil {
		u.finished = true
	}
	return err
}

func (u *remoteUpgradeContext) send(ctx context.Context, op *proto.UpgradeOperation) (*proto.UpgradeOperationResponse, error) {
	u.nextID++
	op.RequestId = u.nextID
	if err := u.stream.Send(&proto.OpenDatabaseClientMessage{Msg: &proto.OpenDatabaseClientMessage_UpgradeOperation{UpgradeOperation: op}}); err != nil {
		return nil, grpcErr(err)
	}
	resp, err := u.stream.Recv()
	if err != nil {
		return nil, grpcErr(err)
	}
	opResp := resp.GetUpgradeOperationResponse()
	if opResp == nil {
		return nil, fmt.Errorf("indexeddb: expected upgrade operation response")
	}
	if opResp.GetRequestId() != op.GetRequestId() {
		return nil, fmt.Errorf("indexeddb: upgrade response request id mismatch")
	}
	if err := rpcStatusErr(opResp.GetError()); err != nil {
		return nil, err
	}
	return opResp, nil
}

type remoteUpgradeDatabase remoteUpgradeContext

func (db *remoteUpgradeDatabase) Name() string { return db.started.GetName() }

func (db *remoteUpgradeDatabase) ObjectStoreNames(ctx context.Context) ([]string, error) {
	return (*remoteUpgradeContext)(db).ObjectStoreNames(ctx)
}

func (db *remoteUpgradeDatabase) CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error {
	return (*remoteUpgradeContext)(db).CreateObjectStore(ctx, name, schema)
}

func (db *remoteUpgradeDatabase) DeleteObjectStore(ctx context.Context, name string) error {
	return (*remoteUpgradeContext)(db).DeleteObjectStore(ctx, name)
}

func (db *remoteUpgradeDatabase) CreateIndex(ctx context.Context, store string, index IndexSchema) error {
	return (*remoteUpgradeContext)(db).CreateIndex(ctx, store, index)
}

func (db *remoteUpgradeDatabase) DeleteIndex(ctx context.Context, store string, name string) error {
	return (*remoteUpgradeContext)(db).DeleteIndex(ctx, store, name)
}

// CreateObjectStore creates a named object store with the supplied schema.
func (db *IndexedDBClient) CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error {
	_, err := db.client.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{
		Name: name, Schema: objectStoreSchemaToProto(schema),
	})
	return grpcErr(err)
}

// DeleteObjectStore removes a named object store.
func (db *IndexedDBClient) DeleteObjectStore(ctx context.Context, name string) error {
	_, err := db.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	return grpcErr(err)
}

// ObjectStore returns a typed handle for working with one object store.
func (db *IndexedDBClient) ObjectStore(name string) *IDBObjectStoreClient {
	return &IDBObjectStoreClient{client: db.client, store: name}
}

// Transaction starts an explicit IndexedDB transaction over the supplied object
// store scope.
func (db *IndexedDBClient) Transaction(ctx context.Context, stores []string, mode TransactionMode, opts TransactionOptions) (*IDBTransactionClient, error) {
	return beginTransaction(ctx, db.client, nil, stores, mode, opts)
}

func beginTransaction(ctx context.Context, client proto.IndexedDBClient, connectionID []byte, stores []string, mode TransactionMode, opts TransactionOptions) (*IDBTransactionClient, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := client.Transaction(streamCtx)
	if err != nil {
		cancel()
		return nil, grpcErr(err)
	}
	if err := stream.Send(&proto.TransactionClientMessage{
		Msg: &proto.TransactionClientMessage_Begin{Begin: &proto.BeginTransactionRequest{
			ConnectionId:   connectionID,
			Stores:         stores,
			Mode:           transactionModeToProto(mode),
			DurabilityHint: durabilityHintToProto(opts.DurabilityHint),
		}},
	}); err != nil {
		_ = stream.CloseSend()
		cancel()
		return nil, grpcErr(err)
	}
	resp, err := stream.Recv()
	if err != nil {
		_ = stream.CloseSend()
		cancel()
		return nil, grpcErr(err)
	}
	if resp.GetBegin() == nil {
		_ = stream.CloseSend()
		cancel()
		return nil, fmt.Errorf("indexeddb: expected transaction begin response")
	}
	return &IDBTransactionClient{stream: stream, cancel: cancel}, nil
}

// IDBObjectStoreClient provides CRUD, range-query, and cursor access to one
// object store.
type IDBObjectStoreClient struct {
	client       proto.IndexedDBClient
	connectionID []byte
	store        string
}

// Get loads one record by primary key.
func (o *IDBObjectStoreClient) Get(ctx context.Context, id string) (Record, error) {
	resp, err := o.client.Get(ctx, &proto.ObjectStoreRequest{ConnectionId: o.connectionID, Store: o.store, Id: id})
	if err != nil {
		return nil, grpcErr(err)
	}
	record, err := recordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

// GetKey resolves the primary key for the supplied lookup id.
func (o *IDBObjectStoreClient) GetKey(ctx context.Context, id string) (string, error) {
	resp, err := o.client.GetKey(ctx, &proto.ObjectStoreRequest{ConnectionId: o.connectionID, Store: o.store, Id: id})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

// Add inserts a new record and fails if its primary key already exists.
func (o *IDBObjectStoreClient) Add(ctx context.Context, record Record) error {
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{ConnectionId: o.connectionID, Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

// Put upserts a record by primary key.
func (o *IDBObjectStoreClient) Put(ctx context.Context, record Record) error {
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{ConnectionId: o.connectionID, Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

// Delete removes one record by primary key.
func (o *IDBObjectStoreClient) Delete(ctx context.Context, id string) error {
	_, err := o.client.Delete(ctx, &proto.ObjectStoreRequest{ConnectionId: o.connectionID, Store: o.store, Id: id})
	return grpcErr(err)
}

// Clear removes every record from the object store.
func (o *IDBObjectStoreClient) Clear(ctx context.Context) error {
	_, err := o.client.Clear(ctx, &proto.ObjectStoreNameRequest{ConnectionId: o.connectionID, Store: o.store})
	return grpcErr(err)
}

// GetAll loads all records that match r.
func (o *IDBObjectStoreClient) GetAll(ctx context.Context, r *KeyRange) ([]Record, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAll(ctx, &proto.ObjectStoreRangeRequest{ConnectionId: o.connectionID, Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcErr(err)
	}
	records, err := recordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

// GetAllKeys loads the primary keys for all records that match r.
func (o *IDBObjectStoreClient) GetAllKeys(ctx context.Context, r *KeyRange) ([]string, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAllKeys(ctx, &proto.ObjectStoreRangeRequest{ConnectionId: o.connectionID, Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcErr(err)
	}
	return resp.GetKeys(), nil
}

// Count returns the number of records that match r.
func (o *IDBObjectStoreClient) Count(ctx context.Context, r *KeyRange) (int64, error) {
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.Count(ctx, &proto.ObjectStoreRangeRequest{ConnectionId: o.connectionID, Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetCount(), nil
}

// DeleteRange removes all records that match r and reports how many were
// deleted.
func (o *IDBObjectStoreClient) DeleteRange(ctx context.Context, r KeyRange) (int64, error) {
	kr, err := krToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.DeleteRange(ctx, &proto.ObjectStoreRangeRequest{ConnectionId: o.connectionID, Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetDeleted(), nil
}

// OpenCursor opens a full-value cursor over the object store.
func (o *IDBObjectStoreClient) OpenCursor(ctx context.Context, r *KeyRange, dir CursorDirection) (*IDBCursorClient, error) {
	return openCursor(ctx, o.client, o.connectionID, o.store, "", r, dir, false, nil)
}

// OpenKeyCursor opens a key-only cursor over the object store.
func (o *IDBObjectStoreClient) OpenKeyCursor(ctx context.Context, r *KeyRange, dir CursorDirection) (*IDBCursorClient, error) {
	return openCursor(ctx, o.client, o.connectionID, o.store, "", r, dir, true, nil)
}

// Index returns a typed handle for a secondary index on the object store.
func (o *IDBObjectStoreClient) Index(name string) *IDBIndexClient {
	return &IDBIndexClient{client: o.client, connectionID: o.connectionID, store: o.store, index: name}
}

// IDBIndexClient provides lookup and cursor access through one secondary index.
type IDBIndexClient struct {
	client       proto.IndexedDBClient
	connectionID []byte
	store        string
	index        string
}

// Get loads the first record that matches the supplied index key.
func (idx *IDBIndexClient) Get(ctx context.Context, values ...any) (Record, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGet(ctx, &proto.IndexQueryRequest{
		ConnectionId: idx.connectionID, Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	record, err := recordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

// GetKey resolves the primary key for the first row that matches values.
func (idx *IDBIndexClient) GetKey(ctx context.Context, values ...any) (string, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return "", err
	}
	resp, err := idx.client.IndexGetKey(ctx, &proto.IndexQueryRequest{
		ConnectionId: idx.connectionID, Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

// GetAll loads every record that matches values and r.
func (idx *IDBIndexClient) GetAll(ctx context.Context, r *KeyRange, values ...any) ([]Record, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAll(ctx, &proto.IndexQueryRequest{
		ConnectionId: idx.connectionID, Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	records, err := recordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

// GetAllKeys loads every primary key that matches values and r.
func (idx *IDBIndexClient) GetAllKeys(ctx context.Context, r *KeyRange, values ...any) ([]string, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAllKeys(ctx, &proto.IndexQueryRequest{
		ConnectionId: idx.connectionID, Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	return resp.GetKeys(), nil
}

// Count returns the number of rows that match values and r.
func (idx *IDBIndexClient) Count(ctx context.Context, r *KeyRange, values ...any) (int64, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexCount(ctx, &proto.IndexQueryRequest{
		ConnectionId: idx.connectionID, Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetCount(), nil
}

// Delete removes all rows that match values.
func (idx *IDBIndexClient) Delete(ctx context.Context, values ...any) (int64, error) {
	return idx.DeleteRange(ctx, nil, values...)
}

// DeleteRange removes all rows that match values and r.
func (idx *IDBIndexClient) DeleteRange(ctx context.Context, r *KeyRange, values ...any) (int64, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexDelete(ctx, &proto.IndexQueryRequest{
		ConnectionId: idx.connectionID, Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetDeleted(), nil
}

// OpenCursor opens a full-value cursor over one secondary index.
func (idx *IDBIndexClient) OpenCursor(ctx context.Context, r *KeyRange, dir CursorDirection, values ...any) (*IDBCursorClient, error) {
	return openCursor(ctx, idx.client, idx.connectionID, idx.store, idx.index, r, dir, false, values)
}

// OpenKeyCursor opens a key-only cursor over one secondary index.
func (idx *IDBIndexClient) OpenKeyCursor(ctx context.Context, r *KeyRange, dir CursorDirection, values ...any) (*IDBCursorClient, error) {
	return openCursor(ctx, idx.client, idx.connectionID, idx.store, idx.index, r, dir, true, values)
}

// IDBTransactionClient is an explicit IndexedDB transaction over a fixed store scope.
type IDBTransactionClient struct {
	stream proto.IndexedDB_TransactionClient
	cancel context.CancelFunc
	mu     sync.Mutex
	nextID uint64
	done   bool
	err    error
}

// ObjectStore returns a transaction-scoped object store handle.
func (tx *IDBTransactionClient) ObjectStore(name string) *IDBTransactionObjectStoreClient {
	return &IDBTransactionObjectStoreClient{tx: tx, store: name}
}

// Commit atomically commits all writes made in the transaction.
func (tx *IDBTransactionClient) Commit(ctx context.Context) error {
	_ = ctx
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return err
		}
		return ErrTransactionDone
	}
	if tx.err != nil {
		err := tx.err
		tx.mu.Unlock()
		return err
	}
	tx.done = true

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Commit{Commit: &proto.TransactionCommitRequest{}}}); err != nil {
		return tx.failLocked(grpcErr(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return tx.failLocked(grpcErr(err))
	}
	commit := resp.GetCommit()
	if commit == nil {
		return tx.failLocked(fmt.Errorf("indexeddb: expected transaction commit response"))
	}
	if err := rpcStatusErr(commit.GetError()); err != nil {
		return tx.failLocked(err)
	}
	tx.mu.Unlock()
	tx.cleanup()
	return nil
}

// Abort rolls back the transaction.
func (tx *IDBTransactionClient) Abort(ctx context.Context) error {
	_ = ctx
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return err
		}
		return ErrTransactionDone
	}
	tx.done = true

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Abort{Abort: &proto.TransactionAbortRequest{}}}); err != nil {
		return tx.failLocked(grpcErr(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return tx.failLocked(grpcErr(err))
	}
	abort := resp.GetAbort()
	if abort == nil {
		return tx.failLocked(fmt.Errorf("indexeddb: expected transaction abort response"))
	}
	if err := rpcStatusErr(abort.GetError()); err != nil {
		return tx.failLocked(err)
	}
	tx.mu.Unlock()
	tx.cleanup()
	return nil
}

func (tx *IDBTransactionClient) sendOperation(op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, ErrTransactionDone
	}
	if tx.err != nil {
		err := tx.err
		tx.mu.Unlock()
		return nil, err
	}
	tx.nextID++
	op.RequestId = tx.nextID

	if err := tx.stream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Operation{Operation: op}}); err != nil {
		return nil, tx.failLocked(grpcErr(err))
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return nil, tx.failLocked(grpcErr(err))
	}
	opResp := resp.GetOperation()
	if opResp == nil {
		return nil, tx.failLocked(fmt.Errorf("indexeddb: expected transaction operation response"))
	}
	if opResp.GetRequestId() != op.GetRequestId() {
		return nil, tx.failLocked(fmt.Errorf("indexeddb: response request id %d does not match %d", opResp.GetRequestId(), op.GetRequestId()))
	}
	if err := rpcStatusErr(opResp.GetError()); err != nil {
		tx.done = true
		tx.err = err
		tx.mu.Unlock()
		tx.cleanup()
		return nil, err
	}
	tx.mu.Unlock()
	return opResp, nil
}

func (tx *IDBTransactionClient) failLocked(err error) error {
	tx.err = err
	tx.done = true
	tx.mu.Unlock()
	tx.cleanup()
	return err
}

func (tx *IDBTransactionClient) cleanup() {
	if tx.stream != nil {
		_ = tx.stream.CloseSend()
		tx.stream = nil
	}
	if tx.cancel != nil {
		tx.cancel()
		tx.cancel = nil
	}
}

// IDBTransactionObjectStoreClient provides transaction-scoped object-store operations.
type IDBTransactionObjectStoreClient struct {
	tx    *IDBTransactionClient
	store string
}

func (s *IDBTransactionObjectStoreClient) Get(ctx context.Context, id string) (Record, error) {
	_ = ctx
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Get{Get: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	if err != nil {
		return nil, err
	}
	record, err := recordFromProto(resp.GetRecord().GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (s *IDBTransactionObjectStoreClient) GetKey(ctx context.Context, id string) (string, error) {
	_ = ctx
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetKey{GetKey: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	if err != nil {
		return "", err
	}
	return resp.GetKey().GetKey(), nil
}

func (s *IDBTransactionObjectStoreClient) Add(ctx context.Context, record Record) error {
	_ = ctx
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Add{Add: &proto.RecordRequest{Store: s.store, Record: pbRecord}}})
	return err
}

func (s *IDBTransactionObjectStoreClient) Put(ctx context.Context, record Record) error {
	_ = ctx
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Put{Put: &proto.RecordRequest{Store: s.store, Record: pbRecord}}})
	return err
}

func (s *IDBTransactionObjectStoreClient) Delete(ctx context.Context, id string) error {
	_ = ctx
	_, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Delete{Delete: &proto.ObjectStoreRequest{Store: s.store, Id: id}}})
	return err
}

func (s *IDBTransactionObjectStoreClient) Clear(ctx context.Context) error {
	_ = ctx
	_, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Clear{Clear: &proto.ObjectStoreNameRequest{Store: s.store}}})
	return err
}

func (s *IDBTransactionObjectStoreClient) GetAll(ctx context.Context, r *KeyRange) ([]Record, error) {
	_ = ctx
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetAll{GetAll: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return nil, err
	}
	records, err := recordsFromProto(resp.GetRecords().GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (s *IDBTransactionObjectStoreClient) GetAllKeys(ctx context.Context, r *KeyRange) ([]string, error) {
	_ = ctx
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_GetAllKeys{GetAllKeys: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return nil, err
	}
	return resp.GetKeys().GetKeys(), nil
}

func (s *IDBTransactionObjectStoreClient) Count(ctx context.Context, r *KeyRange) (int64, error) {
	_ = ctx
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_Count{Count: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return 0, err
	}
	return resp.GetCount().GetCount(), nil
}

func (s *IDBTransactionObjectStoreClient) DeleteRange(ctx context.Context, r KeyRange) (int64, error) {
	_ = ctx
	kr, err := krToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := s.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_DeleteRange{DeleteRange: &proto.ObjectStoreRangeRequest{Store: s.store, Range: kr}}})
	if err != nil {
		return 0, err
	}
	return resp.GetDelete().GetDeleted(), nil
}

func (s *IDBTransactionObjectStoreClient) Index(name string) *IDBTransactionIndexClient {
	return &IDBTransactionIndexClient{tx: s.tx, store: s.store, index: name}
}

// IDBTransactionIndexClient provides transaction-scoped index operations.
type IDBTransactionIndexClient struct {
	tx    *IDBTransactionClient
	store string
	index string
}

func (idx *IDBTransactionIndexClient) Get(ctx context.Context, values ...any) (Record, error) {
	_ = ctx
	req, err := idx.query(nil, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGet{IndexGet: req}})
	if err != nil {
		return nil, err
	}
	record, err := recordFromProto(resp.GetRecord().GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (idx *IDBTransactionIndexClient) GetKey(ctx context.Context, values ...any) (string, error) {
	_ = ctx
	req, err := idx.query(nil, values)
	if err != nil {
		return "", err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGetKey{IndexGetKey: req}})
	if err != nil {
		return "", err
	}
	return resp.GetKey().GetKey(), nil
}

func (idx *IDBTransactionIndexClient) GetAll(ctx context.Context, r *KeyRange, values ...any) ([]Record, error) {
	_ = ctx
	req, err := idx.query(r, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGetAll{IndexGetAll: req}})
	if err != nil {
		return nil, err
	}
	records, err := recordsFromProto(resp.GetRecords().GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (idx *IDBTransactionIndexClient) GetAllKeys(ctx context.Context, r *KeyRange, values ...any) ([]string, error) {
	_ = ctx
	req, err := idx.query(r, values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexGetAllKeys{IndexGetAllKeys: req}})
	if err != nil {
		return nil, err
	}
	return resp.GetKeys().GetKeys(), nil
}

func (idx *IDBTransactionIndexClient) Count(ctx context.Context, r *KeyRange, values ...any) (int64, error) {
	_ = ctx
	req, err := idx.query(r, values)
	if err != nil {
		return 0, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexCount{IndexCount: req}})
	if err != nil {
		return 0, err
	}
	return resp.GetCount().GetCount(), nil
}

func (idx *IDBTransactionIndexClient) Delete(ctx context.Context, values ...any) (int64, error) {
	return idx.DeleteRange(ctx, nil, values...)
}

// DeleteRange removes all transaction-scoped rows that match values and r.
func (idx *IDBTransactionIndexClient) DeleteRange(ctx context.Context, r *KeyRange, values ...any) (int64, error) {
	_ = ctx
	req, err := idx.query(r, values)
	if err != nil {
		return 0, err
	}
	resp, err := idx.tx.sendOperation(&proto.TransactionOperation{Operation: &proto.TransactionOperation_IndexDelete{IndexDelete: req}})
	if err != nil {
		return 0, err
	}
	return resp.GetDelete().GetDeleted(), nil
}

func (idx *IDBTransactionIndexClient) query(r *KeyRange, values []any) (*proto.IndexQueryRequest, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	return &proto.IndexQueryRequest{Store: idx.store, Index: idx.index, Values: vals, Range: kr}, nil
}

// IDBCursorClient streams IndexedDB rows one at a time.
type IDBCursorClient struct {
	stream      proto.IndexedDB_OpenCursorClient
	cancel      context.CancelFunc
	keysOnly    bool
	indexCursor bool
	entry       *proto.CursorEntry
	err         error
	done        bool
}

// Continue advances the cursor by one row.
func (c *IDBCursorClient) Continue() bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Next{Next: true},
	})
}

// ContinueToKey advances the cursor to the supplied key, or exhausts it if the
// key does not exist.
func (c *IDBCursorClient) ContinueToKey(key any) bool {
	kvs, err := cursorKeyToProto(key, c.indexCursor)
	if err != nil {
		c.err = err
		return false
	}
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_ContinueToKey{ContinueToKey: &proto.CursorKeyTarget{Key: kvs}},
	})
}

// Advance skips count rows ahead.
func (c *IDBCursorClient) Advance(count int) bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Advance{Advance: int32(count)},
	})
}

// Key returns the current cursor key.
func (c *IDBCursorClient) Key() any {
	if c.entry == nil || len(c.entry.GetKey()) == 0 {
		return nil
	}
	parts, err := keyValuesToAny(c.entry.GetKey())
	if err != nil {
		c.err = err
		return nil
	}
	if !c.indexCursor && len(parts) == 1 {
		return parts[0]
	}
	return parts
}

// PrimaryKey returns the current record's primary key.
func (c *IDBCursorClient) PrimaryKey() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.GetPrimaryKey()
}

// Value returns the current record.
func (c *IDBCursorClient) Value() (Record, error) {
	if c.keysOnly {
		return nil, ErrKeysOnly
	}
	if c.entry == nil || c.entry.GetRecord() == nil {
		return nil, ErrNotFound
	}
	return recordFromProto(c.entry.GetRecord())
}

// Delete removes the current row and keeps the cursor open.
func (c *IDBCursorClient) Delete() error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return ErrNotFound
	}
	err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Delete{Delete: true},
			},
		},
	})
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("indexeddb: cursor stream ended during mutation"))
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
	case *proto.CursorResponse_Done:
		if v.Done {
			c.done = true
			c.entry = nil
		}
	default:
		return c.setErr(fmt.Errorf("indexeddb: unexpected cursor mutation ack"))
	}
	return nil
}

// Update replaces the current row and keeps the cursor open.
func (c *IDBCursorClient) Update(value Record) error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return ErrNotFound
	}
	pbRecord, err := recordToProto(value)
	if err != nil {
		return fmt.Errorf("indexeddb: marshal cursor update: %w", err)
	}
	err = c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Update{Update: pbRecord},
			},
		},
	})
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("indexeddb: cursor stream ended during mutation"))
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
	case *proto.CursorResponse_Done:
		if v.Done {
			c.done = true
			c.entry = nil
		} else if c.entry != nil {
			c.entry.Record = pbRecord
		}
	default:
		return c.setErr(fmt.Errorf("indexeddb: unexpected cursor mutation ack"))
	}
	return nil
}

// Err returns the terminal cursor error, if any.
func (c *IDBCursorClient) Err() error {
	return c.err
}

func (c *IDBCursorClient) cleanup() error {
	var err error
	if c.stream != nil {
		err = grpcErr(c.stream.CloseSend())
		c.stream = nil
	}
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return err
}

func (c *IDBCursorClient) setErr(err error) error {
	c.err = err
	_ = c.cleanup()
	return c.err
}

// Close closes the cursor stream and releases its transport resources.
func (c *IDBCursorClient) Close() error {
	c.done = true
	c.entry = nil
	if c.stream == nil {
		return c.cleanup()
	}
	sendErr := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Close{Close: true},
			},
		},
	})
	closeErr := c.cleanup()
	if sendErr != nil {
		return grpcErr(sendErr)
	}
	return closeErr
}

func (c *IDBCursorClient) sendAndRecv(cmd *proto.CursorCommand) bool {
	if c.done || c.err != nil {
		return false
	}
	err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: cmd},
	})
	if err != nil {
		_ = c.setErr(grpcErr(err))
		return false
	}
	resp, err := c.stream.Recv()
	if err != nil {
		_ = c.setErr(grpcErr(err))
		return false
	}
	if resp == nil {
		_ = c.setErr(fmt.Errorf("indexeddb: cursor stream ended"))
		return false
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
		return true
	case *proto.CursorResponse_Done:
		if !v.Done {
			_ = c.setErr(fmt.Errorf("indexeddb: unexpected non-exhaustion cursor ack"))
			c.entry = nil
			return false
		}
		c.done = true
		c.entry = nil
		return false
	default:
		_ = c.setErr(fmt.Errorf("indexeddb: unexpected cursor response"))
		c.entry = nil
		return false
	}
}

func cursorDirectionToProto(dir CursorDirection) proto.CursorDirection {
	switch dir {
	case CursorNextUnique:
		return proto.CursorDirection_CURSOR_NEXT_UNIQUE
	case CursorPrev:
		return proto.CursorDirection_CURSOR_PREV
	case CursorPrevUnique:
		return proto.CursorDirection_CURSOR_PREV_UNIQUE
	default:
		return proto.CursorDirection_CURSOR_NEXT
	}
}

func openCursor(ctx context.Context, client proto.IndexedDBClient, connectionID []byte, store, index string, r *KeyRange, dir CursorDirection, keysOnly bool, values []any) (*IDBCursorClient, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	vals, err := typedValuesFromAny(values)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, streamCancel := context.WithCancel(ctx)
	stream, err := client.OpenCursor(streamCtx)
	if err != nil {
		streamCancel()
		return nil, grpcErr(err)
	}
	err = stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Open{
			Open: &proto.OpenCursorRequest{
				ConnectionId: connectionID,
				Store:        store,
				Range:        kr,
				Direction:    cursorDirectionToProto(dir),
				KeysOnly:     keysOnly,
				Index:        index,
				Values:       vals,
			},
		},
	})
	if err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, grpcErr(err)
	}
	// Read the open ack to surface creation errors synchronously.
	resp, err := stream.Recv()
	if err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, grpcErr(err)
	}
	if resp == nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("indexeddb: cursor stream ended during open")
	}
	done, ok := resp.GetResult().(*proto.CursorResponse_Done)
	if !ok || done.Done {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("indexeddb: unexpected cursor open ack")
	}
	return &IDBCursorClient{stream: stream, cancel: streamCancel, keysOnly: keysOnly, indexCursor: index != ""}, nil
}

func krToProto(r *KeyRange) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{LowerOpen: r.LowerOpen, UpperOpen: r.UpperOpen}
	if r.Lower != nil {
		v, err := typedValueFromAny(r.Lower)
		if err != nil {
			return nil, fmt.Errorf("marshal key range lower: %w", err)
		}
		kr.Lower = v
	}
	if r.Upper != nil {
		v, err := typedValueFromAny(r.Upper)
		if err != nil {
			return nil, fmt.Errorf("marshal key range upper: %w", err)
		}
		kr.Upper = v
	}
	return kr, nil
}

func anyToProtoValues(values []any) ([]*proto.TypedValue, error) {
	return typedValuesFromAny(values)
}

func transactionModeToProto(mode TransactionMode) proto.TransactionMode {
	if mode == TransactionReadwrite {
		return proto.TransactionMode_TRANSACTION_READWRITE
	}
	return proto.TransactionMode_TRANSACTION_READONLY
}

func durabilityHintToProto(hint TransactionDurabilityHint) proto.TransactionDurabilityHint {
	switch hint {
	case TransactionDurabilityStrict:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_STRICT
	case TransactionDurabilityRelaxed:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_RELAXED
	default:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_DEFAULT
	}
}

func rpcStatusErr(st *rpcstatus.Status) error {
	if st == nil || st.GetCode() == int32(codes.OK) {
		return nil
	}
	return grpcErr(status.Error(codes.Code(st.GetCode()), st.GetMessage()))
}

func versionChangeInfoFromProto(info *proto.VersionChangeInfo) VersionChangeInfo {
	if info == nil {
		return VersionChangeInfo{}
	}
	return VersionChangeInfo{
		Name:       info.GetName(),
		OldVersion: info.GetOldVersion(),
		NewVersion: info.NewVersion,
		Reason:     versionChangeReasonFromProto(info.GetReason()),
	}
}

func blockedInfoFromProto(info *proto.BlockedInfo) BlockedInfo {
	if info == nil {
		return BlockedInfo{}
	}
	return BlockedInfo{
		Name:               info.GetName(),
		OldVersion:         info.GetOldVersion(),
		NewVersion:         info.NewVersion,
		Reason:             versionChangeReasonFromProto(info.GetReason()),
		OpenConnections:    int(info.GetOpenConnections()),
		ActiveTransactions: int(info.GetActiveOperations()),
	}
}

func versionChangeReasonFromProto(reason proto.VersionChangeReason) VersionChangeReason {
	switch reason {
	case proto.VersionChangeReason_VERSION_CHANGE_REASON_DELETE:
		return VersionChangeDelete
	case proto.VersionChangeReason_VERSION_CHANGE_REASON_UPGRADE:
		return VersionChangeUpgrade
	default:
		return ""
	}
}

func grpcErr(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return ErrNotFound
	case codes.AlreadyExists:
		return ErrAlreadyExists
	case codes.Canceled:
		return ErrAbort
	case codes.InvalidArgument:
		if strings.Contains(st.Message(), "invalid transaction") {
			return ErrInvalidTransaction
		}
		return err
	case codes.FailedPrecondition:
		if strings.Contains(st.Message(), "blocked") {
			return ErrBlocked
		}
		if strings.Contains(st.Message(), "readonly") {
			return ErrReadOnly
		}
		if strings.Contains(st.Message(), "already finished") {
			return ErrTransactionDone
		}
		return err
	default:
		return err
	}
}
