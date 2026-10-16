package providerdrivers

import (
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"gopkg.in/yaml.v3"
)

func prepareYAMLExecution(node yaml.Node, params componentprovider.PrepareParams) (componentprovider.PreparedConfig, error) {
	cfg, err := componentprovider.DecodeYAMLConfig(node, params.Subject)
	if err != nil {
		return componentprovider.PreparedConfig{}, err
	}
	params.Config = cfg
	return componentprovider.PrepareExecution(params)
}
