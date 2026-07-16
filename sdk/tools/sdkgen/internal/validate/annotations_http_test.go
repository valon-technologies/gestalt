package validate

import (
	"testing"

	googleapi "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestParseHTTPRuleRejectsNamedBody(t *testing.T) {
	t.Parallel()

	rule := &googleapi.HttpRule{
		Pattern: &googleapi.HttpRule_Post{Post: "/api/v2/example"},
		Body:    "payload",
	}
	value := protoreflect.ValueOfMessage(rule.ProtoReflect())
	if _, err := parseHTTPRule(value); err == nil {
		t.Fatal("parseHTTPRule: want error for named body")
	} else if err.Error() == "" {
		t.Fatalf("parseHTTPRule error = %v, want non-empty message", err)
	}
}

func TestParseHTTPRuleAllowsStarBody(t *testing.T) {
	t.Parallel()

	rule := &googleapi.HttpRule{
		Pattern: &googleapi.HttpRule_Post{Post: "/api/v2/example"},
		Body:    "*",
	}
	value := protoreflect.ValueOfMessage(rule.ProtoReflect())
	parsed, err := parseHTTPRule(value)
	if err != nil {
		t.Fatalf("parseHTTPRule: %v", err)
	}
	if parsed.Body != "*" || parsed.Verb != "POST" {
		t.Fatalf("parsed rule = %+v, want POST with body *", parsed)
	}
}

func TestParseHTTPRuleAllowsEmptyBody(t *testing.T) {
	t.Parallel()

	desc := (&googleapi.HttpRule{}).ProtoReflect().Descriptor()
	msg := dynamicpb.NewMessage(desc)
	msg.Set(desc.Fields().ByName("get"), protoreflect.ValueOfString("/api/v2/example"))
	value := protoreflect.ValueOfMessage(msg)
	parsed, err := parseHTTPRule(value)
	if err != nil {
		t.Fatalf("parseHTTPRule: %v", err)
	}
	if parsed.Body != "" || parsed.Verb != "GET" {
		t.Fatalf("parsed rule = %+v, want GET with empty body", parsed)
	}
}
