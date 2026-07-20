package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type DeviceBindingRepository interface {
	CreateBinding(ctx context.Context, b *domain.DeviceBinding) error
	GetByDeviceCode(ctx context.Context, deviceCode string) (*domain.DeviceBinding, error)
	GetByID(ctx context.Context, id string) (*domain.DeviceBinding, error)
	ListByPackage(ctx context.Context, packageID string) ([]*domain.DeviceBinding, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.DeviceBinding, error)
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateHeartbeat(ctx context.Context, deviceCode string) error

	CreatePending(ctx context.Context, p *domain.PendingDevice) error
	GetPendingByCode(ctx context.Context, deviceCode string) (*domain.PendingDevice, error)
	DeletePending(ctx context.Context, deviceCode string) error

	CreateComponents(ctx context.Context, components []*domain.DeviceComponent) error
	GetComponentsByCode(ctx context.Context, deviceCode string) ([]*domain.DeviceComponent, error)

	CreateAuditLog(ctx context.Context, log *domain.ActivationAuditLog) error
	ListAuditLogs(ctx context.Context, tenantID string, limit, offset int) ([]*domain.ActivationAuditLog, int64, error)
	ListAuditLogsByPackage(ctx context.Context, packageID string, limit, offset int) ([]*domain.ActivationAuditLog, int64, error)
	DeleteAuditLogsByPackage(ctx context.Context, packageID string) error

	IncrementBoundCount(ctx context.Context, packageID string) error
	DecrementBoundCount(ctx context.Context, packageID string) error
	DeleteByPackage(ctx context.Context, packageID string) error
	UpdateBindingPackage(ctx context.Context, bindingID, newPackageID string) error
}

type MySQLDeviceBindingRepository struct {
	db *gorm.DB
}

func NewMySQLDeviceBindingRepository(db *gorm.DB) *MySQLDeviceBindingRepository {
	return &MySQLDeviceBindingRepository{db: db}
}

func (r *MySQLDeviceBindingRepository) CreateBinding(ctx context.Context, b *domain.DeviceBinding) error {
	return r.db.WithContext(ctx).Create(b).Error
}

func (r *MySQLDeviceBindingRepository) GetByDeviceCode(ctx context.Context, deviceCode string) (*domain.DeviceBinding, error) {
	var b domain.DeviceBinding
	err := r.db.WithContext(ctx).Where("device_code = ? AND status = ?", deviceCode, domain.BindingStatusActive).First(&b).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

func (r *MySQLDeviceBindingRepository) GetByID(ctx context.Context, id string) (*domain.DeviceBinding, error) {
	var b domain.DeviceBinding
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&b).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

func (r *MySQLDeviceBindingRepository) ListByPackage(ctx context.Context, packageID string) ([]*domain.DeviceBinding, error) {
	var bindings []*domain.DeviceBinding
	err := r.db.WithContext(ctx).Where("package_id = ?", packageID).Order("bound_at DESC").Find(&bindings).Error
	return bindings, err
}

func (r *MySQLDeviceBindingRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.DeviceBinding, error) {
	var bindings []*domain.DeviceBinding
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("bound_at DESC").Find(&bindings).Error
	return bindings, err
}

func (r *MySQLDeviceBindingRepository) UpdateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).Model(&domain.DeviceBinding{}).Where("id = ?", id).Update("status", status).Error
}

func (r *MySQLDeviceBindingRepository) UpdateHeartbeat(ctx context.Context, deviceCode string) error {
	return r.db.WithContext(ctx).Model(&domain.DeviceBinding{}).
		Where("device_code = ? AND status = ?", deviceCode, domain.BindingStatusActive).
		Update("last_heartbeat", gorm.Expr("NOW(3)")).Error
}

// --- Pending Devices ---

func (r *MySQLDeviceBindingRepository) CreatePending(ctx context.Context, p *domain.PendingDevice) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *MySQLDeviceBindingRepository) GetPendingByCode(ctx context.Context, deviceCode string) (*domain.PendingDevice, error) {
	var p domain.PendingDevice
	err := r.db.WithContext(ctx).Where("device_code = ?", deviceCode).First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *MySQLDeviceBindingRepository) DeletePending(ctx context.Context, deviceCode string) error {
	return r.db.WithContext(ctx).Where("device_code = ?", deviceCode).Delete(&domain.PendingDevice{}).Error
}

// --- Device Components ---

func (r *MySQLDeviceBindingRepository) CreateComponents(ctx context.Context, components []*domain.DeviceComponent) error {
	if len(components) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&components).Error
}

func (r *MySQLDeviceBindingRepository) GetComponentsByCode(ctx context.Context, deviceCode string) ([]*domain.DeviceComponent, error) {
	var comps []*domain.DeviceComponent
	err := r.db.WithContext(ctx).Where("device_code = ?", deviceCode).Find(&comps).Error
	return comps, err
}

// --- Audit Logs ---

func (r *MySQLDeviceBindingRepository) CreateAuditLog(ctx context.Context, log *domain.ActivationAuditLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *MySQLDeviceBindingRepository) ListAuditLogs(ctx context.Context, tenantID string, limit, offset int) ([]*domain.ActivationAuditLog, int64, error) {
	var logs []*domain.ActivationAuditLog
	var total int64

	q := r.db.WithContext(ctx).Model(&domain.ActivationAuditLog{}).Where("tenant_id = ?", tenantID)
	q.Count(&total)
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&logs).Error
	return logs, total, err
}

func (r *MySQLDeviceBindingRepository) ListAuditLogsByPackage(ctx context.Context, packageID string, limit, offset int) ([]*domain.ActivationAuditLog, int64, error) {
	var logs []*domain.ActivationAuditLog
	var total int64

	q := r.db.WithContext(ctx).Model(&domain.ActivationAuditLog{}).Where("package_id = ?", packageID)
	q.Count(&total)
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&logs).Error
	return logs, total, err
}

func (r *MySQLDeviceBindingRepository) DeleteAuditLogsByPackage(ctx context.Context, packageID string) error {
	return r.db.WithContext(ctx).Where("package_id = ?", packageID).Delete(&domain.ActivationAuditLog{}).Error
}

// --- Package bound_count ---

func (r *MySQLDeviceBindingRepository) IncrementBoundCount(ctx context.Context, packageID string) error {
	return r.db.WithContext(ctx).Model(&domain.DownloadPackage{}).
		Where("id = ?", packageID).
		Update("bound_count", gorm.Expr("bound_count + 1")).Error
}

func (r *MySQLDeviceBindingRepository) DecrementBoundCount(ctx context.Context, packageID string) error {
	return r.db.WithContext(ctx).Model(&domain.DownloadPackage{}).
		Where("id = ? AND bound_count > 0", packageID).
		Update("bound_count", gorm.Expr("bound_count - 1")).Error
}

func (r *MySQLDeviceBindingRepository) DeleteByPackage(ctx context.Context, packageID string) error {
	return r.db.WithContext(ctx).Where("package_id = ?", packageID).Delete(&domain.DeviceBinding{}).Error
}

func (r *MySQLDeviceBindingRepository) UpdateBindingPackage(ctx context.Context, bindingID, newPackageID string) error {
	return r.db.WithContext(ctx).Model(&domain.DeviceBinding{}).
		Where("id = ?", bindingID).
		Update("package_id", newPackageID).Error
}
