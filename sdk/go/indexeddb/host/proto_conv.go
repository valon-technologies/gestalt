package host

import (
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
)

func krToProto(r *idb.KeyRange) (*proto.KeyRange, error) {
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
