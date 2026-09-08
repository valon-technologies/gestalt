package observability

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func TestInstrumentExternalCredentialProviderPreservesOptionalCapabilities(t *testing.T) {
	t.Parallel()

	provider := coretesting.NewStubExternalCredentialProvider()
	instrumented := InstrumentExternalCredentialProvider("test", provider)
	if !core.ExternalCredentialProviderPersistsAccountKey(instrumented) {
		t.Fatal("instrumented provider lost account-key persistence capability")
	}
	conditional, ok := instrumented.(core.ExternalCredentialConditionalUpserter)
	if !ok {
		t.Fatal("instrumented provider lost conditional-upsert capability")
	}
	if err := conditional.UpsertCredentialIfID(context.Background(), &core.ExternalCredential{
		Subject: "user:test", Audience: "slack:default", Qualifier: "workspace",
	}, "missing"); err != core.ErrNotFound {
		t.Fatalf("conditional upsert error = %v, want %v", err, core.ErrNotFound)
	}
}
