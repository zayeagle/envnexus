package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

// MetricType enumerates the supported metric counters.
const (
	MetricDeviceRegistrations = "device_registrations"
	MetricSessions            = "sessions_created"
	MetricLLMCalls            = "llm_calls"
	MetricAuditEvents         = "audit_events"
	MetricApprovals           = "approvals_total"
	MetricWebhookDeliveries   = "webhook_deliveries"
)

type Service struct {
	repo repository.MetricsRepository
}

func NewService(repo repository.MetricsRepository) *Service {
	return &Service{repo: repo}
}

// Increment increments a metric counter for the current calendar month.
func (s *Service) Increment(ctx context.Context, tenantID, metricType string, delta int64) {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0).Add(-time.Nanosecond)

	if err := s.repo.Upsert(ctx, tenantID, metricType, delta, periodStart, periodEnd); err != nil {
		slog.Warn("Failed to increment metric", "tenant_id", tenantID, "metric_type", metricType, "error", err)
	}
}

// GetCurrentPeriod returns usage for the current month.
func (s *Service) GetCurrentPeriod(ctx context.Context, tenantID string) (map[string]int64, error) {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	rows, err := s.repo.ListByTenantAndPeriod(ctx, tenantID, periodStart)
	if err != nil {
		return nil, err
	}

	result := make(map[string]int64)
	for _, r := range rows {
		if r.PeriodStart.Equal(periodStart) {
			result[r.MetricType] = r.Value
		}
	}
	return result, nil
}

// GetHistory returns monthly usage for the past N months.
func (s *Service) GetHistory(ctx context.Context, tenantID string, months int) ([]map[string]interface{}, error) {
	if months < 1 || months > 24 {
		months = 6
	}
	from := time.Now().UTC().AddDate(0, -months, 0)

	rows, err := s.repo.ListByTenantAndPeriod(ctx, tenantID, from)
	if err != nil {
		return nil, err
	}

	byPeriod := make(map[time.Time]map[string]int64)
	for _, r := range rows {
		if _, ok := byPeriod[r.PeriodStart]; !ok {
			byPeriod[r.PeriodStart] = make(map[string]int64)
		}
		byPeriod[r.PeriodStart][r.MetricType] = r.Value
	}

	var result []map[string]interface{}
	for period, m := range byPeriod {
		entry := map[string]interface{}{
			"period": period.Format("2006-01"),
		}
		for k, v := range m {
			entry[k] = v
		}
		result = append(result, entry)
	}
	return result, nil
}
