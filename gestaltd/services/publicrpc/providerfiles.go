package publicrpc

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	protov1 "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// ProviderFiles returns the compiled gestalt.provider.v1 descriptor set used by
// gestaltd, including transitive imports.
func ProviderFiles() (*protoregistry.Files, error) {
	roots := []protoreflect.FileDescriptor{
		protov1.File_v1_agent_proto,
		protov1.File_v1_annotations_proto,
		protov1.File_v1_app_proto,
		protov1.File_v1_authorization_proto,
		protov1.File_v1_cache_proto,
		protov1.File_v1_external_credential_proto,
		protov1.File_v1_identity_proto,
		protov1.File_v1_indexeddb_proto,
		protov1.File_v1_runtime_proto,
		protov1.File_v1_runtime_provider_proto,
		protov1.File_v1_s3_proto,
		protov1.File_v1_secrets_proto,
		protov1.File_v1_test_proto,
		protov1.File_v1_workflow_proto,
	}
	return registerFiles(roots...)
}

func registerFiles(roots ...protoreflect.FileDescriptor) (*protoregistry.Files, error) {
	registry := &protoregistry.Files{}
	var register func(protoreflect.FileDescriptor) error
	register = func(fd protoreflect.FileDescriptor) error {
		if fd == nil {
			return nil
		}
		if _, err := registry.FindFileByPath(fd.Path()); err == nil {
			return nil
		}
		imports := fd.Imports()
		for i := 0; i < imports.Len(); i++ {
			if err := register(imports.Get(i).FileDescriptor); err != nil {
				return err
			}
		}
		if err := registry.RegisterFile(fd); err != nil {
			return fmt.Errorf("register %s: %w", fd.Path(), err)
		}
		return nil
	}

	for _, root := range roots {
		if err := register(root); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
