package validate

import (
	"strings"
	"testing"

	googleapi "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestParseHTTPRuleTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*googleapi.HttpRule)
		wantErr string
	}{
		{
			name: "empty body accepted on GET",
			mutate: func(rule *googleapi.HttpRule) {
				rule.Pattern = &googleapi.HttpRule_Get{Get: "/api/v2/example"}
			},
		},
		{
			name: "star body accepted on POST",
			mutate: func(rule *googleapi.HttpRule) {
				rule.Pattern = &googleapi.HttpRule_Post{Post: "/api/v2/example"}
				rule.Body = "*"
			},
		},
		{
			name: "named body rejected",
			mutate: func(rule *googleapi.HttpRule) {
				rule.Pattern = &googleapi.HttpRule_Post{Post: "/api/v2/example"}
				rule.Body = "payload"
			},
			wantErr: "body",
		},
		{
			name: "additional binding rejected",
			mutate: func(rule *googleapi.HttpRule) {
				rule.Pattern = &googleapi.HttpRule_Post{Post: "/api/v2/example"}
				rule.AdditionalBindings = []*googleapi.HttpRule{
					{Pattern: &googleapi.HttpRule_Get{Get: "/api/v2/other"}},
				}
			},
			wantErr: "additional_bindings",
		},
		{
			name: "custom method rejected",
			mutate: func(rule *googleapi.HttpRule) {
				rule.Pattern = &googleapi.HttpRule_Custom{
					Custom: &googleapi.CustomHttpPattern{Kind: "CUSTOM", Path: "/api/v2/example"},
				}
			},
			wantErr: "custom",
		},
		{
			name: "response body rejected",
			mutate: func(rule *googleapi.HttpRule) {
				rule.Pattern = &googleapi.HttpRule_Get{Get: "/api/v2/example"}
				rule.ResponseBody = "payload"
			},
			wantErr: "response_body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rule := &googleapi.HttpRule{}
			tt.mutate(rule)
			value := protoreflect.ValueOfMessage(rule.ProtoReflect())
			_, err := parseHTTPRule(value)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("parseHTTPRule: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("parseHTTPRule: want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseHTTPRule error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}
