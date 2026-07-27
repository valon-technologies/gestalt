package coredata_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestGestaltdSourceVersionPromotionRetargetsActiveRollouts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)

	if _, err := services.GestaltdSourceVersionState.CurrentForAdmission(ctx); !errors.Is(err, coredata.ErrGestaltdSourceVersionUnavailable) {
		t.Fatalf("CurrentForAdmission before promotion error = %v, want unavailable", err)
	}
	if _, err := services.GestaltdSourceVersionState.Promote(ctx, "source-old", start, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("Promote old: %v", err)
	}
	if _, err := services.AppRollouts.Create(ctx, &core.AppRollout{
		App:                 "g-issues",
		Version:             "v2",
		State:               core.AppRolloutStateEnrolling,
		TargetSourceVersion: "source-old",
		CreatedAt:           start.Add(time.Minute),
		EnrollmentEndsAt:    start.Add(3 * time.Minute),
		Deadline:            start.Add(16 * time.Minute),
	}); err != nil {
		t.Fatalf("Create rollout: %v", err)
	}

	promotedAt := start.Add(5 * time.Minute)
	if _, err := services.GestaltdSourceVersionState.BeginPromotion(ctx, "source-new", promotedAt.Add(-time.Minute)); err != nil {
		t.Fatalf("BeginPromotion: %v", err)
	}
	if _, err := services.GestaltdSourceVersionState.CurrentForAdmission(ctx); !errors.Is(err, coredata.ErrGestaltdSourceVersionPromoting) {
		t.Fatalf("CurrentForAdmission during promotion error = %v, want promoting", err)
	}
	if _, err := services.GestaltdSourceVersionState.CreateAppRollout(ctx, &core.AppRollout{
		App:              "g-slack",
		Version:          "v2",
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        promotedAt,
		EnrollmentEndsAt: promotedAt.Add(2 * time.Minute),
		Deadline:         promotedAt.Add(15 * time.Minute),
	}); !errors.Is(err, coredata.ErrGestaltdSourceVersionPromoting) {
		t.Fatalf("CreateAppRollout during promotion error = %v, want promoting", err)
	}
	if _, err := services.GestaltdSourceVersionState.Promote(ctx, "source-new", promotedAt, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("Promote new: %v", err)
	}

	current, err := services.GestaltdSourceVersionState.CurrentForAdmission(ctx)
	if err != nil {
		t.Fatalf("CurrentForAdmission after promotion: %v", err)
	}
	if current != "source-new" {
		t.Fatalf("current source version = %q, want source-new", current)
	}
	rollout, err := services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.TargetSourceVersion != "source-new" || rollout.State != core.AppRolloutStateEnrolling {
		t.Fatalf("retargeted rollout = %#v", rollout)
	}
	if !rollout.CreatedAt.Equal(promotedAt) ||
		!rollout.EnrollmentEndsAt.Equal(promotedAt.Add(2*time.Minute)) ||
		!rollout.Deadline.Equal(promotedAt.Add(15*time.Minute)) {
		t.Fatalf("retargeted rollout timestamps = %#v", rollout)
	}
}

func TestGestaltdSourceVersionPromotionRejectsDifferentCandidate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)
	if _, err := services.GestaltdSourceVersionState.BeginPromotion(ctx, "source-a", now); err != nil {
		t.Fatalf("BeginPromotion: %v", err)
	}
	if _, err := services.GestaltdSourceVersionState.Promote(ctx, "source-b", now, 2*time.Minute, 15*time.Minute); !errors.Is(err, coredata.ErrGestaltdSourceVersionMismatch) {
		t.Fatalf("Promote error = %v, want mismatch", err)
	}
}

func TestGestaltdSourceVersionCancelPromotionKeepsCurrent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)
	if _, err := services.GestaltdSourceVersionState.Promote(ctx, "source-old", now, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("Promote old: %v", err)
	}
	if _, err := services.GestaltdSourceVersionState.BeginPromotion(ctx, "source-new", now.Add(time.Minute)); err != nil {
		t.Fatalf("BeginPromotion: %v", err)
	}
	if _, err := services.GestaltdSourceVersionState.CancelPromotion(ctx, "source-new", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("CancelPromotion: %v", err)
	}
	current, err := services.GestaltdSourceVersionState.CurrentForAdmission(ctx)
	if err != nil {
		t.Fatalf("CurrentForAdmission: %v", err)
	}
	if current != "source-old" {
		t.Fatalf("current source version = %q, want source-old", current)
	}
}

func TestGestaltdSourceVersionPromotionReopensRolloutCompletedDuringPromotion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)
	if _, err := services.GestaltdSourceVersionState.Promote(ctx, "source-old", start, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("Promote old: %v", err)
	}
	if _, err := services.AppRollouts.Create(ctx, &core.AppRollout{
		App:                 "g-issues",
		Version:             "v2",
		State:               core.AppRolloutStateEnrolling,
		TargetSourceVersion: "source-old",
		CreatedAt:           start,
		EnrollmentEndsAt:    start.Add(2 * time.Minute),
		Deadline:            start.Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	promotionStartedAt := start.Add(3 * time.Minute)
	if _, err := services.GestaltdSourceVersionState.BeginPromotion(ctx, "source-new", promotionStartedAt); err != nil {
		t.Fatalf("BeginPromotion: %v", err)
	}
	if _, err := services.AppRollouts.MarkComplete(ctx, "g-issues", "v2", promotionStartedAt.Add(time.Minute)); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}
	retryAt := promotionStartedAt.Add(2 * time.Minute)
	state, err := services.GestaltdSourceVersionState.BeginPromotion(ctx, "source-new", retryAt)
	if err != nil {
		t.Fatalf("retry BeginPromotion: %v", err)
	}
	if !state.UpdatedAt.Equal(promotionStartedAt) {
		t.Fatalf("promotion started at = %v after retry, want %v", state.UpdatedAt, promotionStartedAt)
	}
	promotedAt := promotionStartedAt.Add(3 * time.Minute)
	if _, err := services.GestaltdSourceVersionState.Promote(ctx, "source-new", promotedAt, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("Promote new: %v", err)
	}
	rollout, err := services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateEnrolling || rollout.TargetSourceVersion != "source-new" ||
		!rollout.CompletedAt.IsZero() || !rollout.CreatedAt.Equal(promotedAt) {
		t.Fatalf("reopened rollout = %#v", rollout)
	}
}
