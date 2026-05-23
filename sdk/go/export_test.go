package gestalt

// TestingHostServiceConnCount returns the number of pooled host-service gRPC connections.
func TestingHostServiceConnCount() int {
	count := 0
	sharedHostServiceConns.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
