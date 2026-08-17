package appregistry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// CommandRunner executes external commands such as gcloud storage.
type CommandRunner interface {
	Run(name string, args ...string) (stdout string, err error)
}

// GcloudObjectStore implements RegistryObjectStore via gcloud storage commands.
type GcloudObjectStore struct {
	Runner CommandRunner
}

func (s *GcloudObjectStore) DescribeObject(storageURL string) (ObjectDescription, error) {
	if s == nil || s.Runner == nil {
		return ObjectDescription{}, fmt.Errorf("gcloud object store runner is required")
	}
	out, err := s.Runner.Run("gcloud", "storage", "objects", "describe", storageURL, "--format=json")
	if err != nil {
		if gcloudObjectNotFound(err) {
			return ObjectDescription{}, nil
		}
		return ObjectDescription{}, err
	}
	var described gcloudObjectDescription
	if err := json.Unmarshal([]byte(out), &described); err != nil {
		return ObjectDescription{}, fmt.Errorf("parse object metadata for %s: %w", storageURL, err)
	}
	return ObjectDescription{
		Generation: int64(described.Generation),
		SHA256:     strings.TrimSpace(described.Metadata["sha256"]),
	}, nil
}

func (s *GcloudObjectStore) ReadObject(storageURL string) (int64, []byte, error) {
	described, err := s.DescribeObject(storageURL)
	if err != nil {
		return 0, nil, err
	}
	if described.Generation == 0 {
		return 0, nil, nil
	}
	out, err := s.Runner.Run("gcloud", "storage", "cat", storageURL)
	if err != nil {
		return 0, nil, err
	}
	return described.Generation, []byte(out), nil
}

func (s *GcloudObjectStore) WriteImmutableObject(input WriteImmutableObjectInput) error {
	metadata := fmt.Sprintf("source-ref=%s", input.SourceRef)
	if input.SHA256 != "" {
		metadata += ",sha256=" + input.SHA256
	}
	_, err := s.Runner.Run(
		"gcloud", "storage", "cp",
		"--if-generation-match=0",
		"--custom-metadata="+metadata,
		input.LocalPath,
		input.StorageURL,
	)
	return err
}

func (s *GcloudObjectStore) WriteCatalogObject(input WriteCatalogObjectInput) error {
	args := []string{
		"storage", "cp",
		"--custom-metadata=source-ref=" + input.SourceRef,
		input.LocalPath,
		input.StorageURL,
	}
	if input.Generation == 0 {
		args = append(args, "--if-generation-match=0")
	} else {
		args = append(args, "--if-generation-match="+strconv.FormatInt(input.Generation, 10))
	}
	_, err := s.Runner.Run("gcloud", args...)
	return err
}

type gcsObjectGeneration int64

func (g *gcsObjectGeneration) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*g = 0
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			*g = 0
			return nil
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("parse generation %q: %w", value, err)
		}
		*g = gcsObjectGeneration(parsed)
		return nil
	}
	var parsed int64
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("parse generation: %w", err)
	}
	*g = gcsObjectGeneration(parsed)
	return nil
}

type gcloudObjectDescription struct {
	Generation gcsObjectGeneration `json:"generation"`
	Metadata   map[string]string   `json:"metadata"`
}

func gcloudObjectNotFound(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "404") ||
		os.IsNotExist(err)
}

func gcloudPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "precondition") ||
		strings.Contains(text, "generation") ||
		strings.Contains(text, "412")
}
