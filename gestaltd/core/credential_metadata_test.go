package core

import "testing"

func TestConnectionParamsFromMetadataJSON_StripsAccountIdentity(t *testing.T) {
	t.Parallel()

	params, err := ConnectionParamsFromMetadataJSON(`{"cloud_id":"abc","account_identity":"{\"facts\":[{\"kind\":\"email\",\"value\":\"a@b.com\",\"primary\":true}]}"}`)
	if err != nil {
		t.Fatal(err)
	}
	if params["cloud_id"] != "abc" {
		t.Fatalf("params = %+v", params)
	}
	if _, ok := params[AccountIdentityMetadataKey]; ok {
		t.Fatal("account_identity must not appear in connection params")
	}
}

func TestConnectionParamsFromMetadataJSON_Empty(t *testing.T) {
	t.Parallel()

	params, err := ConnectionParamsFromMetadataJSON("")
	if err != nil || params != nil {
		t.Fatalf("params=%v err=%v", params, err)
	}
}

func TestAccountKeyFromMetadataJSON_UsesOnlyExplicitKey(t *testing.T) {
	t.Parallel()

	if got := AccountKeyFromMetadataJSON(`{"account_key":" provider:v1:abc ","email":"user@example.com"}`); got != "provider:v1:abc" {
		t.Fatalf("account key = %q, want explicit provider key", got)
	}
	if got := AccountKeyFromMetadataJSON(`{"email":"user@example.com"}`); got != "" {
		t.Fatalf("account key = %q, want empty without explicit key", got)
	}
	if got := AccountKeyFromMetadataJSON(`{"account_key": {"not":"a string"}}`); got != "" {
		t.Fatalf("account key = %q, want empty for malformed metadata", got)
	}
}
