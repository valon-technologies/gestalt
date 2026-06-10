// Package descriptor builds and loads the compiled proto schema (a Buf
// descriptor image) that drives generation.
package descriptor

import (
	"fmt"
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// BuildImage compiles the proto module at protoDir into a descriptor image at
// outPath. Buf images are wire-compatible with FileDescriptorSet and include
// all imports, so the result is self-contained.
func BuildImage(buf *toolchain.Tool, protoDir, outPath string) error {
	return buf.Run(protoDir, "build", "-o", outPath)
}

// Load reads a descriptor image and resolves it into a file registry with
// source location info intact.
func Load(path string) (*protoregistry.Files, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, fds); err != nil {
		return nil, fmt.Errorf("unmarshal descriptor image %s: %w", path, err)
	}
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		return nil, fmt.Errorf("resolve descriptor image %s: %w", path, err)
	}
	return files, nil
}
