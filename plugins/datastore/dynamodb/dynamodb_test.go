package dynamodb

import (
	"context"
	"os"
	"strings"
	"testing"

	awsddb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/uuid"
	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
)

func testEndpoint(t *testing.T) string {
	t.Helper()
	ep := os.Getenv("TOOLSHED_TEST_DYNAMODB_ENDPOINT")
	if ep == "" {
		t.Skip("TOOLSHED_TEST_DYNAMODB_ENDPOINT not set")
	}
	return ep
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	endpoint := testEndpoint(t)
	table := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	store, err := New(Config{
		Table:         table,
		Region:        "us-east-1",
		Endpoint:      endpoint,
		EncryptionKey: coretesting.EncryptionKey(t),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() {
		_, _ = store.client.DeleteTable(context.Background(),
			&awsddb.DeleteTableInput{TableName: &table})
	})

	return store
}

func TestDynamoDBDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}
