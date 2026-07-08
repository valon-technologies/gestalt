package publicrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
	protov1 "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestNormalizePolicyFieldsRejectsDuplicates(t *testing.T) {
	_, err := normalizePolicyFields("fill", []string{"context", "context"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `duplicate field "context"`)
}

func TestValidatePolicyFieldsRejectsUnknownField(t *testing.T) {
	input := (&protov1.AppInvokeRequest{}).ProtoReflect().Descriptor()
	err := validatePolicyFields(input, []string{"not_a_real_field"}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown fill field "not_a_real_field"`)
}

func TestValidatePolicyFieldsRejectsOverlap(t *testing.T) {
	md := protov1.File_v1_app_proto.Services().ByName("App").Methods().ByName("Invoke")
	_, _, err := policyForMethod(md)
	require.NoError(t, err)
}

func TestHasRestrictionParsesCommaSeparatedValues(t *testing.T) {
	require.True(t, hasRestriction("INTERNAL, PUBLIC", "PUBLIC"))
	require.True(t, hasRestriction("PUBLIC", "PUBLIC"))
	require.False(t, hasRestriction("INTERNAL", "PUBLIC"))
}

func TestIntersectReportsSharedFields(t *testing.T) {
	require.Equal(t, []string{"context"}, intersect([]string{"context", "app"}, []string{"context"}))
}
