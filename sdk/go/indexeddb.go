package gestalt

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
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
	// ErrKeysOnly indicates that the current cursor was opened in key-only mode
	// and therefore has no value payload.
	ErrKeysOnly = fmt.Errorf("indexeddb: value not available on key-only cursor")
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
		conn, err = grpc.DialContext(ctx, address,
			append(internalHostServiceBaseDialOptions(
				grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
					MinVersion: tls.VersionTLS12,
					ServerName: host,
					NextProtos: []string{"h2"},
				})),
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

// CreateObjectStore creates a named object store with the supplied schema.
func (db *IndexedDBClient) CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error {
	indexes := make([]*proto.IndexSchema, len(schema.Indexes))
	for i, idx := range schema.Indexes {
		indexes[i] = &proto.IndexSchema{Name: idx.Name, KeyPath: idx.KeyPath, Unique: idx.Unique}
	}
	columns := make([]*proto.ColumnDef, len(schema.Columns))
	for i, col := range schema.Columns {
		columns[i] = &proto.ColumnDef{
			Name:       col.Name,
			Type:       int32(col.Type),
			PrimaryKey: col.PrimaryKey,
			NotNull:    col.NotNull,
			Unique:     col.Unique,
		}
	}
	_, err := db.client.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{
		Name: name, Schema: &proto.ObjectStoreSchema{Indexes: indexes, Columns: columns},
	})
	return grpcErr(err)
}

// DeleteObjectStore removes a named object store.
func (db *IndexedDBClient) DeleteObjectStore(ctx context.Context, name string) error {
	_, err := db.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	return grpcErr(err)
}

// ObjectStore returns a typed handle for working with one object store.
func (db *IndexedDBClient) ObjectStore(name string) *ObjectStoreClient {
	return &ObjectStoreClient{client: db.client, store: name}
}

// ObjectStoreClient provides CRUD, range-query, and cursor access to one
// object store.
type ObjectStoreClient struct {
	client proto.IndexedDBClient
	store  string
}

// Get loads one record by primary key.
func (o *ObjectStoreClient) Get(ctx context.Context, id string) (Record, error) {
	resp, err := o.client.Get(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return nil, grpcErr(err)
	}
	record, err := RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

// GetKey resolves the primary key for the supplied lookup id.
func (o *ObjectStoreClient) GetKey(ctx context.Context, id string) (string, error) {
	resp, err := o.client.GetKey(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

// Add inserts a new record and fails if its primary key already exists.
func (o *ObjectStoreClient) Add(ctx context.Context, record Record) error {
	pbRecord, err := RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

// Put upserts a record by primary key.
func (o *ObjectStoreClient) Put(ctx context.Context, record Record) error {
	pbRecord, err := RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

// Delete removes one record by primary key.
func (o *ObjectStoreClient) Delete(ctx context.Context, id string) error {
	_, err := o.client.Delete(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	return grpcErr(err)
}

// Clear removes every record from the object store.
func (o *ObjectStoreClient) Clear(ctx context.Context) error {
	_, err := o.client.Clear(ctx, &proto.ObjectStoreNameRequest{Store: o.store})
	return grpcErr(err)
}

// GetAll loads all records that match r.
func (o *ObjectStoreClient) GetAll(ctx context.Context, r *KeyRange) ([]Record, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAll(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcErr(err)
	}
	records, err := RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

// GetAllKeys loads the primary keys for all records that match r.
func (o *ObjectStoreClient) GetAllKeys(ctx context.Context, r *KeyRange) ([]string, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAllKeys(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcErr(err)
	}
	return resp.GetKeys(), nil
}

// Count returns the number of records that match r.
func (o *ObjectStoreClient) Count(ctx context.Context, r *KeyRange) (int64, error) {
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.Count(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetCount(), nil
}

// DeleteRange removes all records that match r and reports how many were
// deleted.
func (o *ObjectStoreClient) DeleteRange(ctx context.Context, r KeyRange) (int64, error) {
	kr, err := krToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.DeleteRange(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetDeleted(), nil
}

// OpenCursor opens a full-value cursor over the object store.
func (o *ObjectStoreClient) OpenCursor(ctx context.Context, r *KeyRange, dir CursorDirection) (*Cursor, error) {
	return openCursor(ctx, o.client, o.store, "", r, dir, false, nil)
}

// OpenKeyCursor opens a key-only cursor over the object store.
func (o *ObjectStoreClient) OpenKeyCursor(ctx context.Context, r *KeyRange, dir CursorDirection) (*Cursor, error) {
	return openCursor(ctx, o.client, o.store, "", r, dir, true, nil)
}

// Index returns a typed handle for a secondary index on the object store.
func (o *ObjectStoreClient) Index(name string) *IndexClient {
	return &IndexClient{client: o.client, store: o.store, index: name}
}

// IndexClient provides lookup and cursor access through one secondary index.
type IndexClient struct {
	client proto.IndexedDBClient
	store  string
	index  string
}

// Get loads the first record that matches the supplied index key.
func (idx *IndexClient) Get(ctx context.Context, values ...any) (Record, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGet(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	record, err := RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

// GetKey resolves the primary key for the first row that matches values.
func (idx *IndexClient) GetKey(ctx context.Context, values ...any) (string, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return "", err
	}
	resp, err := idx.client.IndexGetKey(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

// GetAll loads every record that matches values and r.
func (idx *IndexClient) GetAll(ctx context.Context, r *KeyRange, values ...any) ([]Record, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAll(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	records, err := RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

// GetAllKeys loads every primary key that matches values and r.
func (idx *IndexClient) GetAllKeys(ctx context.Context, r *KeyRange, values ...any) ([]string, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAllKeys(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	return resp.GetKeys(), nil
}

// Count returns the number of rows that match values and r.
func (idx *IndexClient) Count(ctx context.Context, r *KeyRange, values ...any) (int64, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexCount(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetCount(), nil
}

// Delete removes all rows that match values.
func (idx *IndexClient) Delete(ctx context.Context, values ...any) (int64, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexDelete(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetDeleted(), nil
}

// OpenCursor opens a full-value cursor over one secondary index.
func (idx *IndexClient) OpenCursor(ctx context.Context, r *KeyRange, dir CursorDirection, values ...any) (*Cursor, error) {
	return openCursor(ctx, idx.client, idx.store, idx.index, r, dir, false, values)
}

// OpenKeyCursor opens a key-only cursor over one secondary index.
func (idx *IndexClient) OpenKeyCursor(ctx context.Context, r *KeyRange, dir CursorDirection, values ...any) (*Cursor, error) {
	return openCursor(ctx, idx.client, idx.store, idx.index, r, dir, true, values)
}

// Cursor streams IndexedDB rows one at a time.
type Cursor struct {
	stream      proto.IndexedDB_OpenCursorClient
	cancel      context.CancelFunc
	keysOnly    bool
	indexCursor bool
	entry       *proto.CursorEntry
	err         error
	done        bool
}

// Continue advances the cursor by one row.
func (c *Cursor) Continue() bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Next{Next: true},
	})
}

// ContinueToKey advances the cursor to the supplied key, or exhausts it if the
// key does not exist.
func (c *Cursor) ContinueToKey(key any) bool {
	kvs, err := CursorKeyToProto(key, c.indexCursor)
	if err != nil {
		c.err = err
		return false
	}
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_ContinueToKey{ContinueToKey: &proto.CursorKeyTarget{Key: kvs}},
	})
}

// Advance skips count rows ahead.
func (c *Cursor) Advance(count int) bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Advance{Advance: int32(count)},
	})
}

// Key returns the current cursor key.
func (c *Cursor) Key() any {
	if c.entry == nil || len(c.entry.GetKey()) == 0 {
		return nil
	}
	parts, err := KeyValuesToAny(c.entry.GetKey())
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
func (c *Cursor) PrimaryKey() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.GetPrimaryKey()
}

// Value returns the current record.
func (c *Cursor) Value() (Record, error) {
	if c.keysOnly {
		return nil, ErrKeysOnly
	}
	if c.entry == nil || c.entry.GetRecord() == nil {
		return nil, ErrNotFound
	}
	return RecordFromProto(c.entry.GetRecord())
}

// Delete removes the current row and keeps the cursor open.
func (c *Cursor) Delete() error {
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
func (c *Cursor) Update(value Record) error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return ErrNotFound
	}
	pbRecord, err := RecordToProto(value)
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
func (c *Cursor) Err() error {
	return c.err
}

func (c *Cursor) cleanup() error {
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

func (c *Cursor) setErr(err error) error {
	c.err = err
	_ = c.cleanup()
	return c.err
}

// Close closes the cursor stream and releases its transport resources.
func (c *Cursor) Close() error {
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

func (c *Cursor) sendAndRecv(cmd *proto.CursorCommand) bool {
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

func openCursor(ctx context.Context, client proto.IndexedDBClient, store, index string, r *KeyRange, dir CursorDirection, keysOnly bool, values []any) (*Cursor, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	vals, err := TypedValuesFromAny(values)
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
				Store:     store,
				Range:     kr,
				Direction: cursorDirectionToProto(dir),
				KeysOnly:  keysOnly,
				Index:     index,
				Values:    vals,
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
	return &Cursor{stream: stream, cancel: streamCancel, keysOnly: keysOnly, indexCursor: index != ""}, nil
}

func krToProto(r *KeyRange) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{LowerOpen: r.LowerOpen, UpperOpen: r.UpperOpen}
	if r.Lower != nil {
		v, err := TypedValueFromAny(r.Lower)
		if err != nil {
			return nil, fmt.Errorf("marshal key range lower: %w", err)
		}
		kr.Lower = v
	}
	if r.Upper != nil {
		v, err := TypedValueFromAny(r.Upper)
		if err != nil {
			return nil, fmt.Errorf("marshal key range upper: %w", err)
		}
		kr.Upper = v
	}
	return kr, nil
}

func anyToProtoValues(values []any) ([]*proto.TypedValue, error) {
	return TypedValuesFromAny(values)
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
	default:
		return err
	}
}
