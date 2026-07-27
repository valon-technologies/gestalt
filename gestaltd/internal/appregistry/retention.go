package appregistry

import (
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

const (
	DefaultUnusedRetention   = 72 * time.Hour
	DefaultDeployedRetention = 720 * time.Hour
	FirstInstallFromVersion  = "registry:first-install"
)

const (
	DeploymentStateAvailable    = "available"
	DeploymentStateExpired      = "expired"
	DeploymentStateDesired      = "desired"
	DeploymentStateRedeployable = "redeployable"
	DeploymentStateLocked       = "locked"
)

type RetentionPolicy struct {
	UnusedRetention   time.Duration
	DeployedRetention time.Duration
}

func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		UnusedRetention:   DefaultUnusedRetention,
		DeployedRetention: DefaultDeployedRetention,
	}
}

type VersionDeploymentChain struct {
	Deployed  map[string]struct{}
	Deadlines VersionDeploymentDeadlines
}

type VersionDeploymentDeadlines map[string]time.Time

func VersionDeploymentChainFromChangeRequests(requests []*core.AppVersionChangeRequest) VersionDeploymentChain {
	return VersionDeploymentChain{
		Deployed:  DeployedVersionsFromChangeRequests(requests),
		Deadlines: DeployableUntilDeadlinesFromChangeRequests(requests),
	}
}

func DeployableUntilDeadlinesFromChangeRequests(requests []*core.AppVersionChangeRequest) VersionDeploymentDeadlines {
	deadlines := VersionDeploymentDeadlines{}
	if len(requests) == 0 {
		return deadlines
	}
	sorted := append([]*core.AppVersionChangeRequest(nil), requests...)
	sort.Slice(sorted, func(i, j int) bool {
		left := sorted[i]
		right := sorted[j]
		if left == nil || right == nil {
			return left != nil
		}
		if left.Timestamp.Equal(right.Timestamp) {
			return strings.TrimSpace(left.ID) < strings.TrimSpace(right.ID)
		}
		return left.Timestamp.Before(right.Timestamp)
	})
	for _, request := range sorted {
		if request == nil {
			continue
		}
		fromVersion := strings.TrimSpace(request.FromVersion)
		if fromVersion == "" || fromVersion == FirstInstallFromVersion {
			continue
		}
		if request.FromVersionDeployableUntil == nil || request.FromVersionDeployableUntil.IsZero() {
			continue
		}
		deadline := request.FromVersionDeployableUntil.UTC()
		deadlines[fromVersion] = deadline
	}
	return deadlines
}

// DeployedVersionsFromChangeRequests returns versions that entered the deploy chain.
func DeployedVersionsFromChangeRequests(requests []*core.AppVersionChangeRequest) map[string]struct{} {
	out := map[string]struct{}{}
	for _, request := range requests {
		if request == nil {
			continue
		}
		if version := strings.TrimSpace(request.ToVersion); version != "" {
			out[version] = struct{}{}
		}
		fromVersion := strings.TrimSpace(request.FromVersion)
		if fromVersion != "" && fromVersion != FirstInstallFromVersion {
			out[fromVersion] = struct{}{}
		}
	}
	return out
}

func VersionDeploymentState(version string, desiredVersion string, publishedAt time.Time, policy RetentionPolicy, now time.Time, chain VersionDeploymentChain) (state string, deployableUntil *time.Time) {
	version = strings.TrimSpace(version)
	desiredVersion = strings.TrimSpace(desiredVersion)
	now = now.UTC()
	if version == desiredVersion && version != "" {
		return DeploymentStateDesired, nil
	}
	if chain.Deployed != nil {
		if _, wasDeployed := chain.Deployed[version]; wasDeployed {
			deadline := chain.deployableUntil(version)
			if deadline != nil && now.Before(deadline.UTC()) {
				return DeploymentStateRedeployable, cloneTimePtr(deadline)
			}
			if deadline != nil && !deadline.IsZero() {
				return DeploymentStateLocked, cloneTimePtr(deadline)
			}
			return DeploymentStateLocked, nil
		}
	}
	if !publishedAt.IsZero() {
		unusedDeadline := publishedAt.UTC().Add(policy.UnusedRetention)
		if now.Before(unusedDeadline) {
			return DeploymentStateAvailable, nil
		}
		return DeploymentStateExpired, nil
	}
	return DeploymentStateAvailable, nil
}

func (c VersionDeploymentChain) deployableUntil(version string) *time.Time {
	if len(c.Deadlines) == 0 {
		return nil
	}
	deadline, ok := c.Deadlines[strings.TrimSpace(version)]
	if !ok || deadline.IsZero() {
		return nil
	}
	return cloneTimePtr(&deadline)
}

func VersionSelectable(version, desiredVersion string, publishedAt time.Time, policy RetentionPolicy, now time.Time, chain VersionDeploymentChain) error {
	state, _ := VersionDeploymentState(version, desiredVersion, publishedAt, policy, now, chain)
	switch state {
	case DeploymentStateAvailable, DeploymentStateRedeployable:
		return nil
	case DeploymentStateDesired:
		return ErrAppVersionAlreadyInstalled
	case DeploymentStateExpired:
		return ErrAppVersionExpired
	case DeploymentStateLocked:
		return ErrAppVersionLocked
	default:
		return ErrAppVersionExpired
	}
}

func UnusedVersionExpired(publishedAt time.Time, policy RetentionPolicy, now time.Time) bool {
	if publishedAt.IsZero() {
		return false
	}
	return !now.UTC().Before(publishedAt.UTC().Add(policy.UnusedRetention))
}
