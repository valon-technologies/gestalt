package authzappresource

import "testing"

func TestResolverSingletonResourceCategoryA(t *testing.T) {
	t.Parallel()

	r := NewResolver(map[string]struct{}{
		"dealHub": {},
	}, map[string]string{
		"brain": "brainPolicy",
	})

	resource := r.SingletonResource("dataSchemaExplorer")
	if resource.GetType() != TypeApp || resource.GetId() != "dataSchemaExplorer" {
		t.Fatalf("resource = %#v, want app:dataSchemaExplorer", resource)
	}

	resource = r.SingletonResource("brain")
	if resource.GetType() != TypeApp || resource.GetId() != "brainPolicy" {
		t.Fatalf("resource = %#v, want app:brainPolicy", resource)
	}

	resource = r.SingletonResourceByID("dealHub")
	if resource.GetType() != "dealHub" || resource.GetId() != "dealHub" {
		t.Fatalf("resource = %#v, want dealHub:dealHub", resource)
	}
}
