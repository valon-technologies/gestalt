package scim

import (
	"encoding/json"
	"fmt"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

func decodeJSONValue(value any, target any) error {
	if value == nil {
		return fmt.Errorf("value is missing")
	}
	data, ok := value.([]byte)
	if !ok {
		if raw, rawOK := value.(json.RawMessage); rawOK {
			data = raw
		} else {
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			data = encoded
		}
	}
	return json.Unmarshal(data, target)
}

func recordString(record idb.Record, key string) string {
	if record == nil || record[key] == nil {
		return ""
	}
	switch value := record[key].(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}

func recordBool(record idb.Record, key string) bool {
	value, _ := record[key].(bool)
	return value
}

func recordInt(record idb.Record, key string) int64 {
	switch value := record[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case int32:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func recordTime(record idb.Record, key string) time.Time {
	switch value := record[key].(type) {
	case time.Time:
		return value
	case *time.Time:
		if value != nil {
			return *value
		}
	}
	return time.Time{}
}
