package quota

import (
	"encoding/json"
	"time"
)

type codexQuotaSnapshot struct {
	PlanType                         string                              `json:"plan_type,omitempty"`
	ActiveLimit                      string                              `json:"active_limit,omitempty"`
	PrimaryUsedPercent               int                                 `json:"primary_used_percent"`
	PrimaryUsedPercentExact          *float64                            `json:"primary_used_percent_exact,omitempty"`
	PrimaryWindowMinutes             int                                 `json:"primary_window_minutes,omitempty"`
	PrimaryResetAt                   time.Time                           `json:"primary_reset_at,omitempty"`
	SecondaryUsedPercent             int                                 `json:"secondary_used_percent"`
	SecondaryUsedPercentExact        *float64                            `json:"secondary_used_percent_exact,omitempty"`
	SecondaryWindowMinutes           int                                 `json:"secondary_window_minutes,omitempty"`
	SecondaryResetAt                 time.Time                           `json:"secondary_reset_at,omitempty"`
	PrimaryOverSecondaryLimitPercent int                                 `json:"primary_over_secondary_limit_percent,omitempty"`
	CreditsHasCredits                bool                                `json:"credits_has_credits"`
	CreditsUnlimited                 bool                                `json:"credits_unlimited"`
	CreditsBalance                   string                              `json:"credits_balance,omitempty"`
	DetailedLimits                   []codexQuotaLimitSnapshot           `json:"detailed_limits,omitempty"`
	RateLimitResetCredits            *codexRateLimitResetCreditsSnapshot `json:"rate_limit_reset_credits,omitempty"`
	UpdatedAt                        time.Time                           `json:"updated_at"`
}

type codexRateLimitResetCreditsSnapshot struct {
	AvailableCount   int                                 `json:"available_count"`
	TotalEarnedCount int                                 `json:"total_earned_count,omitempty"`
	CreatedAt        *time.Time                          `json:"created_at,omitempty"`
	ExpiresAt        *time.Time                          `json:"expires_at,omitempty"`
	Credits          []codexRateLimitResetCreditSnapshot `json:"credits,omitempty"`
}

type codexRateLimitResetCreditSnapshot struct {
	ID        string     `json:"id,omitempty"`
	Title     string     `json:"title,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type codexQuotaLimitSnapshot struct {
	LimitID                          string    `json:"limit_id"`
	LimitName                        string    `json:"limit_name,omitempty"`
	PrimaryUsedPercent               int       `json:"primary_used_percent"`
	PrimaryUsedPercentExact          *float64  `json:"primary_used_percent_exact,omitempty"`
	PrimaryWindowMinutes             int       `json:"primary_window_minutes,omitempty"`
	PrimaryResetAt                   time.Time `json:"primary_reset_at,omitempty"`
	PrimaryResetAfterSeconds         int       `json:"primary_reset_after_seconds,omitempty"`
	SecondaryUsedPercent             int       `json:"secondary_used_percent"`
	SecondaryUsedPercentExact        *float64  `json:"secondary_used_percent_exact,omitempty"`
	SecondaryWindowMinutes           int       `json:"secondary_window_minutes,omitempty"`
	SecondaryResetAt                 time.Time `json:"secondary_reset_at,omitempty"`
	SecondaryResetAfterSeconds       int       `json:"secondary_reset_after_seconds,omitempty"`
	PrimaryOverSecondaryLimitPercent int       `json:"primary_over_secondary_limit_percent,omitempty"`
}

func encodeCodexQuotaSnapshot(info *CodexQuotaInfo) string {
	if info == nil {
		return ""
	}
	snapshot := codexQuotaSnapshotFromInfo(info)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeCodexQuotaSnapshot(raw string) *CodexQuotaInfo {
	if raw == "" {
		return nil
	}
	var snapshot codexQuotaSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil
	}
	return snapshot.toInfo()
}

func codexQuotaSnapshotFromInfo(info *CodexQuotaInfo) codexQuotaSnapshot {
	snapshot := codexQuotaSnapshot{
		PlanType:                         info.PlanType,
		ActiveLimit:                      info.ActiveLimit,
		PrimaryUsedPercent:               info.PrimaryUsedPercent,
		PrimaryUsedPercentExact:          cloneFloat64Ptr(info.PrimaryUsedPercentExact),
		PrimaryWindowMinutes:             info.PrimaryWindowMinutes,
		PrimaryResetAt:                   info.PrimaryResetAt,
		SecondaryUsedPercent:             info.SecondaryUsedPercent,
		SecondaryUsedPercentExact:        cloneFloat64Ptr(info.SecondaryUsedPercentExact),
		SecondaryWindowMinutes:           info.SecondaryWindowMinutes,
		SecondaryResetAt:                 info.SecondaryResetAt,
		PrimaryOverSecondaryLimitPercent: info.PrimaryOverSecondaryLimitPercent,
		CreditsHasCredits:                info.CreditsHasCredits,
		CreditsUnlimited:                 info.CreditsUnlimited,
		CreditsBalance:                   info.CreditsBalance,
		RateLimitResetCredits:            codexRateLimitResetCreditsSnapshotFromInfo(info.RateLimitResetCredits),
		UpdatedAt:                        info.UpdatedAt,
	}
	if len(info.DetailedLimits) > 0 {
		snapshot.DetailedLimits = make([]codexQuotaLimitSnapshot, 0, len(info.DetailedLimits))
		for _, limit := range info.DetailedLimits {
			snapshot.DetailedLimits = append(snapshot.DetailedLimits, codexQuotaLimitSnapshot{
				LimitID:                          limit.LimitID,
				LimitName:                        limit.LimitName,
				PrimaryUsedPercent:               limit.PrimaryUsedPercent,
				PrimaryUsedPercentExact:          cloneFloat64Ptr(limit.PrimaryUsedPercentExact),
				PrimaryWindowMinutes:             limit.PrimaryWindowMinutes,
				PrimaryResetAt:                   limit.PrimaryResetAt,
				PrimaryResetAfterSeconds:         limit.PrimaryResetAfterSeconds,
				SecondaryUsedPercent:             limit.SecondaryUsedPercent,
				SecondaryUsedPercentExact:        cloneFloat64Ptr(limit.SecondaryUsedPercentExact),
				SecondaryWindowMinutes:           limit.SecondaryWindowMinutes,
				SecondaryResetAt:                 limit.SecondaryResetAt,
				SecondaryResetAfterSeconds:       limit.SecondaryResetAfterSeconds,
				PrimaryOverSecondaryLimitPercent: limit.PrimaryOverSecondaryLimitPercent,
			})
		}
	}
	return snapshot
}

func (s codexQuotaSnapshot) toInfo() *CodexQuotaInfo {
	info := &CodexQuotaInfo{
		PlanType:                         s.PlanType,
		ActiveLimit:                      s.ActiveLimit,
		PrimaryUsedPercent:               s.PrimaryUsedPercent,
		PrimaryUsedPercentExact:          cloneFloat64Ptr(s.PrimaryUsedPercentExact),
		PrimaryWindowMinutes:             s.PrimaryWindowMinutes,
		PrimaryResetAt:                   s.PrimaryResetAt,
		SecondaryUsedPercent:             s.SecondaryUsedPercent,
		SecondaryUsedPercentExact:        cloneFloat64Ptr(s.SecondaryUsedPercentExact),
		SecondaryWindowMinutes:           s.SecondaryWindowMinutes,
		SecondaryResetAt:                 s.SecondaryResetAt,
		PrimaryOverSecondaryLimitPercent: s.PrimaryOverSecondaryLimitPercent,
		CreditsHasCredits:                s.CreditsHasCredits,
		CreditsUnlimited:                 s.CreditsUnlimited,
		CreditsBalance:                   s.CreditsBalance,
		RateLimitResetCredits:            s.RateLimitResetCredits.toInfo(),
		UpdatedAt:                        s.UpdatedAt,
	}
	if len(s.DetailedLimits) > 0 {
		info.DetailedLimits = make([]CodexQuotaLimitInfo, 0, len(s.DetailedLimits))
		for _, limit := range s.DetailedLimits {
			info.DetailedLimits = append(info.DetailedLimits, CodexQuotaLimitInfo{
				LimitID:                          limit.LimitID,
				LimitName:                        limit.LimitName,
				PrimaryUsedPercent:               limit.PrimaryUsedPercent,
				PrimaryUsedPercentExact:          cloneFloat64Ptr(limit.PrimaryUsedPercentExact),
				PrimaryWindowMinutes:             limit.PrimaryWindowMinutes,
				PrimaryResetAt:                   limit.PrimaryResetAt,
				PrimaryResetAfterSeconds:         limit.PrimaryResetAfterSeconds,
				SecondaryUsedPercent:             limit.SecondaryUsedPercent,
				SecondaryUsedPercentExact:        cloneFloat64Ptr(limit.SecondaryUsedPercentExact),
				SecondaryWindowMinutes:           limit.SecondaryWindowMinutes,
				SecondaryResetAt:                 limit.SecondaryResetAt,
				SecondaryResetAfterSeconds:       limit.SecondaryResetAfterSeconds,
				PrimaryOverSecondaryLimitPercent: limit.PrimaryOverSecondaryLimitPercent,
			})
		}
	}
	return info
}

func codexRateLimitResetCreditsSnapshotFromInfo(info *CodexRateLimitResetCreditsInfo) *codexRateLimitResetCreditsSnapshot {
	if info == nil {
		return nil
	}
	snapshot := &codexRateLimitResetCreditsSnapshot{
		AvailableCount:   info.AvailableCount,
		TotalEarnedCount: info.TotalEarnedCount,
		CreatedAt:        cloneTimePtr(info.CreatedAt),
		ExpiresAt:        cloneTimePtr(info.ExpiresAt),
	}
	if len(info.Credits) > 0 {
		snapshot.Credits = make([]codexRateLimitResetCreditSnapshot, 0, len(info.Credits))
		for _, credit := range info.Credits {
			snapshot.Credits = append(snapshot.Credits, codexRateLimitResetCreditSnapshot{
				ID:        credit.ID,
				Title:     credit.Title,
				CreatedAt: cloneTimePtr(credit.CreatedAt),
				ExpiresAt: cloneTimePtr(credit.ExpiresAt),
			})
		}
	}
	return snapshot
}

func (s *codexRateLimitResetCreditsSnapshot) toInfo() *CodexRateLimitResetCreditsInfo {
	if s == nil {
		return nil
	}
	info := &CodexRateLimitResetCreditsInfo{
		AvailableCount:   s.AvailableCount,
		TotalEarnedCount: s.TotalEarnedCount,
		CreatedAt:        cloneTimePtr(s.CreatedAt),
		ExpiresAt:        cloneTimePtr(s.ExpiresAt),
	}
	if len(s.Credits) > 0 {
		info.Credits = make([]CodexRateLimitResetCredit, 0, len(s.Credits))
		for _, credit := range s.Credits {
			info.Credits = append(info.Credits, CodexRateLimitResetCredit{
				ID:        credit.ID,
				Title:     credit.Title,
				CreatedAt: cloneTimePtr(credit.CreatedAt),
				ExpiresAt: cloneTimePtr(credit.ExpiresAt),
			})
		}
	}
	return info
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
