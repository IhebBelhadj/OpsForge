package httpapi

import (
	"context"

	"github.com/IhebBelhadj/opsforge/academy/internal/rewards"
)

// Awards is what the transport needs from the rewards module.
type Awards interface {
	Summary(ctx context.Context, user int64) (rewards.Summary, error)
}

// GetRewards is the learner's XP and badges.
func (s *server) GetRewards(ctx context.Context, _ GetRewardsRequestObject) (GetRewardsResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	sum, err := s.deps.Awards.Summary(ctx, user)
	if err != nil {
		return nil, err
	}
	out := GetRewards200JSONResponse{Xp: sum.XP, Badges: make([]Badge, len(sum.Badges))}
	for i, b := range sum.Badges {
		courses := b.Courses
		if courses == nil {
			courses = []string{}
		}
		out.Badges[i] = Badge{Id: b.ID, Pack: b.Pack, Title: b.Title, Description: b.Description, Icon: b.Icon,
			Courses: courses, Retired: b.Retired, AwardedAt: b.AwardedAt}
	}
	return out, nil
}
