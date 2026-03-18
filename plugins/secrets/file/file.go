package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/toolshed/core"
)

type Provider struct {
	dir string
}

func (p *Provider) GetSecret(_ context.Context, name string) (string, error) {
	cleaned := filepath.Clean(filepath.Join(p.dir, name))
	rel, err := filepath.Rel(p.dir, cleaned)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("secret name %q escapes base directory", name)
	}

	data, err := os.ReadFile(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: file %q", core.ErrSecretNotFound, name)
		}
		return "", fmt.Errorf("reading secret file: %w", err)
	}
	return strings.TrimRight(string(data), "\n\r"), nil
}
