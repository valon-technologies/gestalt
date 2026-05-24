package indexeddb

import (
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func toProtoValues(values []any) ([]*proto.TypedValue, error) {
	return typedValuesFromAny(values)
}

func transactionModeToProto(mode idb.TransactionMode) proto.TransactionMode {
	if mode == idb.TransactionReadwrite {
		return proto.TransactionMode_TRANSACTION_READWRITE
	}
	return proto.TransactionMode_TRANSACTION_READONLY
}

func durabilityHintToProto(hint idb.TransactionDurabilityHint) proto.TransactionDurabilityHint {
	switch hint {
	case idb.TransactionDurabilityStrict:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_STRICT
	case idb.TransactionDurabilityRelaxed:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_RELAXED
	default:
		return proto.TransactionDurabilityHint_TRANSACTION_DURABILITY_DEFAULT
	}
}

func rpcStatusToDatastoreErr(st *rpcstatus.Status) error {
	if st == nil || st.GetCode() == int32(codes.OK) {
		return nil
	}
	return idb.RPCError(status.Error(codes.Code(st.GetCode()), st.GetMessage()))
}

func keyRangeToProto(r *idb.KeyRange) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{
		LowerOpen: r.LowerOpen,
		UpperOpen: r.UpperOpen,
	}
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

// --- Remote Cursor ---

func cursorDirectionToProto(dir idb.CursorDirection) proto.CursorDirection {
	switch dir {
	case idb.CursorNextUnique:
		return proto.CursorDirection_CURSOR_NEXT_UNIQUE
	case idb.CursorPrev:
		return proto.CursorDirection_CURSOR_PREV
	case idb.CursorPrevUnique:
		return proto.CursorDirection_CURSOR_PREV_UNIQUE
	default:
		return proto.CursorDirection_CURSOR_NEXT
	}
}
