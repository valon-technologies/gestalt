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
