package bootstrap

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/services/observability"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	hostedAgentRuntimeReasonOK               = "ok"
	hostedAgentRuntimeReasonError            = "error"
	hostedAgentRuntimeReasonCanceled         = "canceled"
	hostedAgentRuntimeReasonDeadlineExceeded = "deadline_exceeded"
)

func recordHostedAgentRuntimeInstances(ctx context.Context, providerName string, ready, starting, draining int) {
	observability.RecordAgentRuntimeInstances(
		ctx,
		int64(ready),
		int64(starting),
		int64(draining),
		observability.AttrAgentProvider.String(providerName),
	)
}

func recordHostedAgentRuntimeStartPhase(ctx context.Context, providerName, phase string, startedAt time.Time, err error) {
	observability.RecordAgentRuntimeStart(
		ctx,
		startedAt,
		err != nil,
		hostedAgentRuntimeAttrs(providerName, phase, err)...,
	)
}

func recordHostedAgentRuntimeHealthCheck(ctx context.Context, providerName string, startedAt time.Time, err error) {
	observability.RecordAgentRuntimeHealthCheck(
		ctx,
		startedAt,
		err != nil,
		hostedAgentRuntimeAttrs(providerName, "", err)...,
	)
}

func recordHostedAgentRuntimeReplacement(ctx context.Context, providerName string, err error) {
	observability.RecordAgentRuntimeReplacement(
		ctx,
		err != nil,
		hostedAgentRuntimeAttrs(providerName, "", err)...,
	)
}

func hostedAgentRuntimeAttrs(providerName, phase string, err error) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		observability.AttrAgentProvider.String(providerName),
		observability.AttrAgentRuntimeReason.String(hostedAgentRuntimeReason(err)),
	}
	if strings.TrimSpace(phase) != "" {
		attrs = append(attrs, observability.AttrAgentRuntimePhase.String(phase))
	}
	return attrs
}

func hostedAgentRuntimeReason(err error) string {
	if err == nil {
		return hostedAgentRuntimeReasonOK
	}
	if errors.Is(err, context.Canceled) {
		return hostedAgentRuntimeReasonCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return hostedAgentRuntimeReasonDeadlineExceeded
	}
	code := status.Code(err)
	if code != codes.OK && code != codes.Unknown {
		return strings.ToLower(code.String())
	}
	return hostedAgentRuntimeReasonError
}
