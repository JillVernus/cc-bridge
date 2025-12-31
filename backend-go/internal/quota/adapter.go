// Package quota provides rate limit tracking for OpenAI/Codex channels
package quota

import (
	"time"

	"github.com/JillVernus/cc-bridge/internal/requestlog"
)

// RequestLogAdapter adapts requestlog.Manager to the Persister interface
type RequestLogAdapter struct {
	manager *requestlog.Manager
}

// NewRequestLogAdapter creates a new adapter for requestlog.Manager
func NewRequestLogAdapter(manager *requestlog.Manager) *RequestLogAdapter {
	return &RequestLogAdapter{manager: manager}
}

// SaveChannelQuota implements Persister
func (a *RequestLogAdapter) SaveChannelQuota(q *PersistedQuota) error {
	cq := &requestlog.ChannelQuota{
		ChannelID:              q.ChannelID,
		ChannelName:            q.ChannelName,
		PlanType:               q.PlanType,
		PrimaryUsedPercent:     q.PrimaryUsedPercent,
		PrimaryWindowMinutes:   q.PrimaryWindowMinutes,
		PrimaryResetAt:         q.PrimaryResetAt,
		SecondaryUsedPercent:   q.SecondaryUsedPercent,
		SecondaryWindowMinutes: q.SecondaryWindowMinutes,
		SecondaryResetAt:       q.SecondaryResetAt,
		CreditsHasCredits:      q.CreditsHasCredits,
		CreditsUnlimited:       q.CreditsUnlimited,
		CreditsBalance:         q.CreditsBalance,
		IsExceeded:             q.IsExceeded,
		ExceededAt:             q.ExceededAt,
		RecoverAt:              q.RecoverAt,
		ExceededReason:         q.ExceededReason,
		UpdatedAt:              q.UpdatedAt,
	}
	return a.manager.SaveChannelQuota(cq)
}

// GetChannelQuota implements Persister
func (a *RequestLogAdapter) GetChannelQuota(channelID int) (*PersistedQuota, error) {
	cq, err := a.manager.GetChannelQuota(channelID)
	if err != nil || cq == nil {
		return nil, err
	}
	return convertToPersistedQuota(cq), nil
}

// GetAllChannelQuotas implements Persister
func (a *RequestLogAdapter) GetAllChannelQuotas() ([]*PersistedQuota, error) {
	cqs, err := a.manager.GetAllChannelQuotas()
	if err != nil {
		return nil, err
	}
	result := make([]*PersistedQuota, len(cqs))
	for i, cq := range cqs {
		result[i] = convertToPersistedQuota(cq)
	}
	return result, nil
}

func convertToPersistedQuota(cq *requestlog.ChannelQuota) *PersistedQuota {
	var updatedAt time.Time
	if !cq.UpdatedAt.IsZero() {
		updatedAt = cq.UpdatedAt
	}
	return &PersistedQuota{
		ChannelID:              cq.ChannelID,
		ChannelName:            cq.ChannelName,
		PlanType:               cq.PlanType,
		PrimaryUsedPercent:     cq.PrimaryUsedPercent,
		PrimaryWindowMinutes:   cq.PrimaryWindowMinutes,
		PrimaryResetAt:         cq.PrimaryResetAt,
		SecondaryUsedPercent:   cq.SecondaryUsedPercent,
		SecondaryWindowMinutes: cq.SecondaryWindowMinutes,
		SecondaryResetAt:       cq.SecondaryResetAt,
		CreditsHasCredits:      cq.CreditsHasCredits,
		CreditsUnlimited:       cq.CreditsUnlimited,
		CreditsBalance:         cq.CreditsBalance,
		IsExceeded:             cq.IsExceeded,
		ExceededAt:             cq.ExceededAt,
		RecoverAt:              cq.RecoverAt,
		ExceededReason:         cq.ExceededReason,
		UpdatedAt:              updatedAt,
	}
}
