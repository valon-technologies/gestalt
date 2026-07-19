package publicrpc

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	gestaltproto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// GeneratedFiles returns a file registry containing the generated Gestalt
// provider service descriptors used for public RPC discovery.
func GeneratedFiles() (*protoregistry.Files, error) {
	return RegisterFiles(
		gestaltproto.File_v1_app_proto,
		gestaltproto.File_v1_agent_proto,
		gestaltproto.File_v1_workflow_proto,
		gestaltproto.File_v1_indexeddb_proto,
		gestaltproto.File_v1_identity_proto,
		gestaltproto.File_v1_authorization_proto,
		gestaltproto.File_v1_external_credential_proto,
		gestaltproto.File_v1_remote_proto,
	)
}

// RegisterFiles registers the given file descriptors and their imports into a
// new file registry.
func RegisterFiles(roots ...protoreflect.FileDescriptor) (*protoregistry.Files, error) {
	registry := &protoregistry.Files{}
	seen := map[string]struct{}{}
	var register func(protoreflect.FileDescriptor) error
	register = func(fd protoreflect.FileDescriptor) error {
		if fd == nil {
			return nil
		}
		path := fd.Path()
		if _, ok := seen[path]; ok {
			return nil
		}
		imports := fd.Imports()
		for i := 0; i < imports.Len(); i++ {
			if err := register(imports.Get(i).FileDescriptor); err != nil {
				return err
			}
		}
		if err := registry.RegisterFile(fd); err != nil {
			return fmt.Errorf("register %s: %w", path, err)
		}
		seen[path] = struct{}{}
		return nil
	}
	for _, root := range roots {
		if err := register(root); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
