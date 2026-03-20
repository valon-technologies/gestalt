package core

type Capability struct {
	Provider    string
	Operation   string
	Description string
	Parameters  []Parameter
}
