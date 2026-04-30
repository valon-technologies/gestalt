package providerhost

import plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"

type InvocationTokenManager = plugininvokerservice.InvocationTokenManager

func NewInvocationTokenManager(secret []byte) (*InvocationTokenManager, error) {
	return plugininvokerservice.NewInvocationTokenManager(secret)
}
