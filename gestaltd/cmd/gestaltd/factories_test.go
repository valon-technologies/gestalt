package main

import "testing"

func TestBuildFactoriesRegistersFileAPI(t *testing.T) {
	t.Parallel()

	if buildFactories().FileAPI == nil {
		t.Fatal("buildFactories().FileAPI is nil")
	}
}
