package sandbox

type Policy struct {
	ReadOnlyPaths  []string
	ReadWritePaths []string
	AllowedHosts   []string
	ProxyPort      int
	HostBinary     string
}
