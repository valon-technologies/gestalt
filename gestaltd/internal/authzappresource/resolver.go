package authzappresource

import (
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const TypeApp = "app"

// Resolver maps mounted app singleton authorization resources.
type Resolver struct {
	dedicatedSingletonTypes map[string]struct{}
	appAuthorizationPolicy  map[string]string
}

func NewResolver(dedicatedSingletonTypes map[string]struct{}, appAuthorizationPolicy map[string]string) *Resolver {
	if dedicatedSingletonTypes == nil {
		dedicatedSingletonTypes = map[string]struct{}{}
	}
	if appAuthorizationPolicy == nil {
		appAuthorizationPolicy = map[string]string{}
	}
	return &Resolver{
		dedicatedSingletonTypes: dedicatedSingletonTypes,
		appAuthorizationPolicy:  appAuthorizationPolicy,
	}
}

func IsDedicatedSingletonResourceType(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == TypeApp {
		return false
	}
	if strings.Contains(name, ".") || strings.Contains(name, "/") {
		return false
	}
	switch name {
	case "managedSubject", "service_account":
		return false
	default:
		return true
	}
}

func (r *Resolver) AuthorizationResourceID(appName string) string {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return ""
	}
	if r != nil {
		if policy, ok := r.appAuthorizationPolicy[appName]; ok && policy != "" {
			return policy
		}
	}
	return appName
}

func (r *Resolver) SingletonResource(appName string) *proto.Resource {
	return r.SingletonResourceByID(r.AuthorizationResourceID(appName))
}

func (r *Resolver) SingletonResourceByID(resourceID string) *proto.Resource {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil
	}
	if r != nil {
		if _, dedicated := r.dedicatedSingletonTypes[resourceID]; dedicated {
			return &proto.Resource{Type: resourceID, Id: resourceID}
		}
	}
	return &proto.Resource{Type: TypeApp, Id: resourceID}
}

func (r *Resolver) DefaultRoleResourceTypeName(resourceID string) string {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return TypeApp
	}
	if r != nil {
		if _, dedicated := r.dedicatedSingletonTypes[resourceID]; dedicated {
			return resourceID
		}
	}
	return TypeApp
}
