package workflowwire

import (
	"reflect"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

func TestEncodeValueTemplateUsesFlatTemplateShape(t *testing.T) {
	t.Parallel()

	got := EncodeValue(coreworkflow.Value{
		Template: &coreworkflow.Text{Template: "hello {{.name}}"},
	})
	want := map[string]any{"template": "hello {{.name}}"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EncodeValue() = %#v, want %#v", got, want)
	}
}
