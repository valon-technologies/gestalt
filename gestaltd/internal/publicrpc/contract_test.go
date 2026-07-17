package publicrpc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"google.golang.org/genproto/googleapis/api/annotations"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type surfaceContract struct {
	Counts struct {
		Services         int `json:"services"`
		GRPCUnaryMethods int `json:"grpcUnaryMethods"`
		RESTMethods      int `json:"restMethods"`
	} `json:"counts"`
	Services []struct {
		Name     string `json:"name"`
		FullName string `json:"fullName"`
		Methods  []struct {
			Name        string   `json:"name"`
			FullMethod  string   `json:"fullMethod"`
			Input       string   `json:"input"`
			Output      string   `json:"output"`
			InputEmpty  bool     `json:"inputEmpty"`
			OutputEmpty bool     `json:"outputEmpty"`
			Transports  []string `json:"transports"`
			Fill        []string `json:"fill"`
			Reject      []string `json:"reject"`
			HTTP        *struct {
				Verb string `json:"verb"`
				Path string `json:"path"`
			} `json:"http"`
		} `json:"methods"`
	} `json:"services"`
}

func loadSurfaceContract(t *testing.T) surfaceContract {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	path := filepath.Join(root, "sdk", "testdata", "public_conformance", "surface_v1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read surface_v1.json: %v", err)
	}
	var contract surfaceContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("unmarshal surface_v1.json: %v", err)
	}
	return contract
}

func TestRegistryMatchesSurfaceContract(t *testing.T) {
	t.Parallel()

	contract := loadSurfaceContract(t)
	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}

	methods := registry.Methods()
	if got, want := len(methods), contract.Counts.GRPCUnaryMethods; got != want {
		t.Fatalf("registry methods = %d, want %d from contract", got, want)
	}

	grpcOnly := 0
	for _, svc := range contract.Services {
		for _, method := range svc.Methods {
			policy, ok := registry.Lookup(method.FullMethod)
			if !ok {
				t.Fatalf("registry missing contract method %s", method.FullMethod)
			}
			if !slices.Equal(policy.Fill, method.Fill) {
				t.Fatalf("%s Fill = %#v, want %#v", method.FullMethod, policy.Fill, method.Fill)
			}
			if !slices.Equal(policy.Reject, method.Reject) {
				t.Fatalf("%s Reject = %#v, want %#v", method.FullMethod, policy.Reject, method.Reject)
			}
			hasREST := slices.Contains(method.Transports, "rest")
			hasGRPC := slices.Contains(method.Transports, "grpc")
			if !hasGRPC {
				t.Fatalf("%s missing grpc transport in contract", method.FullMethod)
			}
			if hasREST {
				if method.HTTP == nil {
					t.Fatalf("%s has rest transport but no HTTP binding", method.FullMethod)
				}
			} else {
				grpcOnly++
			}
		}
	}
	if got, want := grpcOnly, contract.Counts.GRPCUnaryMethods-contract.Counts.RESTMethods; got != want {
		t.Fatalf("gRPC-only methods = %d, want %d", got, want)
	}
}

func TestDescriptorRESTRoutesMatchSurfaceContract(t *testing.T) {
	t.Parallel()

	contract := loadSurfaceContract(t)
	want := contractRESTRoutes(contract)
	got, err := descriptorRESTRoutes()
	if err != nil {
		t.Fatalf("descriptorRESTRoutes: %v", err)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("REST contract:\n got: %s\nwant: %s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func contractRESTRoutes(contract surfaceContract) []string {
	var routes []string
	for _, svc := range contract.Services {
		for _, method := range svc.Methods {
			if method.HTTP == nil {
				continue
			}
			routes = append(routes, method.HTTP.Verb+" "+method.HTTP.Path+" "+method.FullMethod)
		}
	}
	return routes
}

func descriptorRESTRoutes() ([]string, error) {
	files, err := publicrpc.GeneratedFiles()
	if err != nil {
		return nil, err
	}
	public, err := publicrpc.NewRegistry(files)
	if err != nil {
		return nil, err
	}

	var routes []string
	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if file.Package() != "gestalt.provider.v1" {
			return true
		}
		services := file.Services()
		for i := 0; i < services.Len(); i++ {
			methods := services.Get(i).Methods()
			for j := 0; j < methods.Len(); j++ {
				method := methods.Get(j)
				options := method.Options()
				if !gproto.HasExtension(options, annotations.E_Http) {
					continue
				}
				fullMethod := "/" + string(method.Parent().FullName()) + "/" + string(method.Name())
				if _, ok := public.Lookup(fullMethod); !ok {
					continue
				}
				rule := gproto.GetExtension(options, annotations.E_Http).(*annotations.HttpRule)
				if len(rule.GetAdditionalBindings()) != 0 {
					continue
				}
				verb, path := httpRuleFromDescriptor(rule)
				if verb == "" || path == "" {
					continue
				}
				routes = append(routes, verb+" "+path+" "+fullMethod)
			}
		}
		return true
	})
	return routes, nil
}

func httpRuleFromDescriptor(rule *annotations.HttpRule) (string, string) {
	for _, candidate := range []struct {
		verb string
		path string
	}{
		{"GET", rule.GetGet()},
		{"PUT", rule.GetPut()},
		{"POST", rule.GetPost()},
		{"DELETE", rule.GetDelete()},
		{"PATCH", rule.GetPatch()},
	} {
		if candidate.path != "" {
			return candidate.verb, candidate.path
		}
	}
	return "", ""
}

func TestRegistryRESTBindingsArePublic(t *testing.T) {
	t.Parallel()

	contract := loadSurfaceContract(t)
	public, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}

	for _, svc := range contract.Services {
		for _, method := range svc.Methods {
			if method.HTTP == nil {
				continue
			}
			if _, ok := public.Lookup(method.FullMethod); !ok {
				t.Fatalf("REST-bound method %s is not public", method.FullMethod)
			}
		}
	}
}

func substitutePath(path string) string {
	replacements := map[string]string{
		"{app}":           "test-app",
		"{operation}":     "test-operation",
		"{session_id}":    "session-1",
		"{turn_id}":       "turn-1",
		"{definition_id}": "definition-1",
		"{activation_id}": "activation-1",
		"{run_id}":        "run-1",
		"{grant_id}":      "grant-1",
	}
	out := path
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	return out
}
