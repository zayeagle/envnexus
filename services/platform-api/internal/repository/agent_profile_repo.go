package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type AgentProfileRepository interface {
	GetByID(ctx context.Context, id string, tenantID string) (*domain.AgentProfile, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.AgentProfile, error)
	Create(ctx context.Context, profile *domain.AgentProfile) error
	Update(ctx context.Context, profile *domain.AgentProfile) error
	Delete(ctx context.Context, id string, tenantID string) error
}

type MySQLAgentProfileRepository struct {
	db *gorm.DB
}

func NewMySQLAgentProfileRepository(db *gorm.DB) *MySQLAgentProfileRepository {
	return &MySQLAgentProfileRepository{db: db}
}

func (r *MySQLAgentProfileRepository) GetByID(ctx context.Context, id string, tenantID string) (*domain.AgentProfile, error) {
	var profile domain.AgentProfile
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", id, tenantID).First(&profile).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &profile, nil
}

func (r *MySQLAgentProfileRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.AgentProfile, error) {
	var profiles []*domain.AgentProfile
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND deleted_at IS NULL", tenantID).Find(&profiles).Error
	if err != nil {
		return nil, err
	}
	return profiles, nil
}

func (r *MySQLAgentProfileRepository) Create(ctx context.Context, profile *domain.AgentProfile) error {
	return r.db.WithContext(ctx).Create(profile).Error
}

func (r *MySQLAgentProfileRepository) Update(ctx context.Context, profile *domain.AgentProfile) error {
	return r.db.WithContext(ctx).Save(profile).Error
}

func (r *MySQLAgentProfileRepository) Delete(ctx context.Context, id string, tenantID string) error {
	return r.db.WithContext(ctx).Model(&domain.AgentProfile{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("deleted_at", gorm.Expr("NOW()")).Error
}
