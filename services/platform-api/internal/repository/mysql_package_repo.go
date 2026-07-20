package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type PackageBuildSecrets struct {
	ActivationKey   string
	EnrollmentToken string
}

type PackageRepository interface {
	Create(ctx context.Context, pkg *domain.DownloadPackage, secrets ...PackageBuildSecrets) error
	GetByID(ctx context.Context, id string) (*domain.DownloadPackage, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.DownloadPackage, error)
	GetByActivationKeyHash(ctx context.Context, keyHash string) (*domain.DownloadPackage, error)
	UpdateMaxDevices(ctx context.Context, packageID string, maxDevices int) error
	Delete(ctx context.Context, id, tenantID string) error
}

type MySQLPackageRepository struct {
	db *gorm.DB
}

func NewMySQLPackageRepository(db *gorm.DB) *MySQLPackageRepository {
	return &MySQLPackageRepository{db: db}
}

func (r *MySQLPackageRepository) Create(ctx context.Context, pkg *domain.DownloadPackage, secrets ...PackageBuildSecrets) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(pkg).Error; err != nil {
			return err
		}

		payload := map[string]string{
			"package_id": pkg.ID,
			"tenant_id":  pkg.TenantID,
			"platform":   pkg.Platform,
			"arch":       pkg.Arch,
			"version":    pkg.Version,
		}
		if pkg.PackageType != "" {
			payload["package_type"] = pkg.PackageType
		}
		if pkg.ActivationMode != "" {
			payload["activation_mode"] = pkg.ActivationMode
		}
		if len(secrets) > 0 {
			if secrets[0].ActivationKey != "" {
				payload["activation_key"] = secrets[0].ActivationKey
			}
			if secrets[0].EnrollmentToken != "" {
				payload["enrollment_token"] = secrets[0].EnrollmentToken
			}
		}
		payloadBytes, _ := json.Marshal(payload)
		payloadStr := string(payloadBytes)

		job := map[string]interface{}{
			"id":           ulid.Make().String(),
			"tenant_id":    pkg.TenantID,
			"job_type":     "package_build",
			"status":       "queued",
			"payload_json": payloadStr,
			"priority":     5,
			"max_attempts": 3,
			"scheduled_at": time.Now(),
			"created_at":   time.Now(),
		}

		return tx.Table("jobs").Create(job).Error
	})
}

func (r *MySQLPackageRepository) GetByID(ctx context.Context, id string) (*domain.DownloadPackage, error) {
	var pkg domain.DownloadPackage
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&pkg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &pkg, nil
}

func (r *MySQLPackageRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.DownloadPackage, error) {
	var pkgs []*domain.DownloadPackage
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&pkgs).Error
	return pkgs, err
}

func (r *MySQLPackageRepository) GetByActivationKeyHash(ctx context.Context, keyHash string) (*domain.DownloadPackage, error) {
	var pkg domain.DownloadPackage
	err := r.db.WithContext(ctx).Where("activation_key_hash = ? AND activation_key_hash != ''", keyHash).First(&pkg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &pkg, nil
}

func (r *MySQLPackageRepository) UpdateMaxDevices(ctx context.Context, packageID string, maxDevices int) error {
	return r.db.WithContext(ctx).Model(&domain.DownloadPackage{}).Where("id = ?", packageID).Update("max_devices", maxDevices).Error
}

func (r *MySQLPackageRepository) Delete(ctx context.Context, id, tenantID string) error {
	result := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&domain.DownloadPackage{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
