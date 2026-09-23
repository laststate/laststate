package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// OnboardingStep represents an onboarding step.
type OnboardingStep struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Step        string    `json:"step"`
	CompletedAt time.Time `json:"completed_at"`
}

// OnboardingStatus represents the onboarding status for a user.
type OnboardingStatus struct {
	UserID   uuid.UUID        `json:"user_id"`
	Steps    []OnboardingStep `json:"steps"`
	Progress float64          `json:"progress"` // 0.0 to 1.0
}

// OnboardingProgress represents the onboarding progress for a user.
type OnboardingProgress struct {
	UserID     uuid.UUID `json:"user_id"`
	Completed  int       `json:"completed"`
	Total      int       `json:"total"`
	Percentage float64   `json:"percentage"` // 0 to 100
}

// GetOnboardingStatus returns the onboarding status for a user.
func (s *Store) GetOnboardingStatus(ctx context.Context, userID uuid.UUID) (*OnboardingStatus, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, user_id, step, completed_at FROM onboarding_steps WHERE user_id=$1 ORDER BY step`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var steps []OnboardingStep
	for rows.Next() {
		var step OnboardingStep
		if err := rows.Scan(&step.ID, &step.UserID, &step.Step, &step.CompletedAt); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if steps == nil {
		steps = []OnboardingStep{}
	}
	// Calculate progress.
	totalSteps := 5 // signup, org_create, invite_members, first_event, mfa_setup
	completed := len(steps)
	progress := float64(completed) / float64(totalSteps)
	return &OnboardingStatus{
		UserID:   userID,
		Steps:    steps,
		Progress: progress,
	}, nil
}

// MarkOnboardingStep marks an onboarding step as completed.
func (s *Store) MarkOnboardingStep(ctx context.Context, userID uuid.UUID, step string) error {
	_, err := s.Pool.Exec(ctx, `
INSERT INTO onboarding_steps(user_id, step, completed_at)
VALUES ($1, $2, now())
ON CONFLICT (user_id, step) DO UPDATE SET completed_at=now()`,
		userID, step)
	return err
}

// GetOnboardingProgress returns the onboarding progress for a user.
func (s *Store) GetOnboardingProgress(ctx context.Context, userID uuid.UUID) (*OnboardingProgress, error) {
	var completed int
	err := s.Pool.QueryRow(ctx, `
SELECT COUNT(*) FROM onboarding_steps WHERE user_id=$1`, userID).Scan(&completed)
	if err != nil {
		return nil, err
	}
	totalSteps := 5
	percentage := float64(completed) / float64(totalSteps) * 100
	return &OnboardingProgress{
		UserID:     userID,
		Completed:  completed,
		Total:      totalSteps,
		Percentage: percentage,
	}, nil
}
