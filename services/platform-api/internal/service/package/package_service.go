package package_svc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/infrastructure"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	device_binding "github.com/zayeagle/envnexus/services/platform-api/internal/service/device_binding"
)

type Service struct {
	pkgRepo     repository.PackageRepository
	enrollRepo  repository.EnrollmentRepository
	bindingRepo repository.DeviceBindingRepository
	minioClient *infrastructure.MinIOClient
}

func NewService(pkgRepo repository.PackageRepository, enrollRepo repository.EnrollmentRepository, bindingRepo repository.DeviceBindingRepository, minioClient *infrastructure.MinIOClient) *Service {
	return &Service{
		pkgRepo:     pkgRepo,
		enrollRepo:  enrollRepo,
		bindingRepo: bindingRepo,
		minioClient: minioClient,
	}
}

func isDuplicateDownloadPackageKey(err error) bool {
	var my *mysql.MySQLError
	if !errors.As(err, &my) || my.Number != 1062 {
		return false
	}
	return strings.Contains(my.Message, "uk_download_packages_profile_platform")
}

func (s *Service) CreatePackage(ctx context.Context, tenantID string, req dto.CreatePackageRequest) (*dto.PackageResponse, error) {
	packageType := req.PackageType
	if packageType == "" {
		packageType = "installer"
	}

	typeLabel := ""
	if packageType == "portable" {
		typeLabel = "-Portable"
	}
	pkgName := fmt.Sprintf("EnvNexus-Agent%s-%s-%s-%s.zip", typeLabel, req.Platform, req.Arch, req.Version)
	artifactPath := fmt.Sprintf("packages/%s/%s", tenantID, pkgName)

	activationMode := req.ActivationMode
	if activationMode == "" {
		activationMode = domain.ActivationModeAuto
	}
	maxDevices := req.MaxDevices
	if maxDevices <= 0 {
		maxDevices = 1
	}

	var activationKey string
	var activationKeyHash string
	if activationMode == domain.ActivationModeAuto || activationMode == domain.ActivationModeBoth {
		activationKey = device_binding.GenerateActivationKey()
		activationKeyHash = device_binding.HashActivationKey(activationKey)
	}

	pkg := &domain.DownloadPackage{
		ID:                ulid.Make().String(),
		TenantID:          tenantID,
		AgentProfileID:    req.AgentProfileID,
		DistributionMode:  req.DistributionMode,
		Platform:          req.Platform,
		Arch:              req.Arch,
		Version:           req.Version,
		PackageType:       packageType,
		PackageName:       pkgName,
		DownloadURL:       "",
		ArtifactPath:      artifactPath,
		Checksum:          "",
		SignStatus:        "pending",
		Status:            "pending",
		ActivationMode:    activationMode,
		ActivationKeyHash: activationKeyHash,
		MaxDevices:        maxDevices,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	enrollTokenStr, err := s.createEnrollmentTokenForPackage(ctx, tenantID, req.AgentProfileID, pkg.ID, maxDevices)
	if err != nil {
		return nil, fmt.Errorf("failed to create enrollment token: %w", err)
	}

	if err := s.pkgRepo.Create(ctx, pkg, repository.PackageBuildSecrets{
		ActivationKey:   activationKey,
		EnrollmentToken: enrollTokenStr,
	}); err != nil {
		if isDuplicateDownloadPackageKey(err) {
			return nil, domain.ErrDuplicateDownloadPackage
		}
		return nil, err
	}

	resp := s.toResponse(ctx, pkg)
	if activationKey != "" {
		resp.ActivationKey = activationKey
	}
	return resp, nil
}

func (s *Service) createEnrollmentTokenForPackage(ctx context.Context, tenantID, agentProfileID, packageID string, maxUses int) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	tokenStr := "enx_tok_" + hex.EncodeToString(tokenBytes)
	hash := sha256.Sum256([]byte(tokenStr))
	tokenHash := hex.EncodeToString(hash[:])

	now := time.Now()
	token := &domain.EnrollmentToken{
		ID:                ulid.Make().String(),
		TenantID:          tenantID,
		AgentProfileID:    agentProfileID,
		DownloadPackageID: packageID,
		TokenHash:         tokenHash,
		Channel:           "stable",
		MaxUses:           maxUses,
		UsedCount:         0,
		ExpiresAt:         now.Add(365 * 24 * time.Hour),
		Status:            "active",
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.enrollRepo.Create(ctx, token); err != nil {
		return "", err
	}
	return tokenStr, nil
}

func (s *Service) ListPackages(ctx context.Context, tenantID string) ([]*dto.PackageResponse, error) {
	pkgs, err := s.pkgRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var res []*dto.PackageResponse
	for _, pkg := range pkgs {
		res = append(res, s.toResponse(ctx, pkg))
	}
	return res, nil
}

func (s *Service) DeletePackage(ctx context.Context, tenantID, packageID string) error {
	// 1. Check if there are any active bindings
	bindings, err := s.bindingRepo.ListByPackage(ctx, packageID)
	if err != nil {
		return err
	}
	
	for _, b := range bindings {
		if b.Status == domain.BindingStatusActive {
			return errors.New("cannot delete package: there are active device bindings")
		}
	}

	// 2. Delete all bindings (even revoked ones) to satisfy foreign key constraints
	if err := s.bindingRepo.DeleteByPackage(ctx, packageID); err != nil {
		return err
	}

	// 3. Delete all audit logs for this package to satisfy foreign key constraints (if any)
	if err := s.bindingRepo.DeleteAuditLogsByPackage(ctx, packageID); err != nil {
		return err
	}

	// 4. Delete the package
	return s.pkgRepo.Delete(ctx, packageID, tenantID)
}

func (s *Service) GetPresignedURL(ctx context.Context, tenantID, packageID string) (string, error) {
	pkg, err := s.pkgRepo.GetByID(ctx, packageID)
	if err != nil {
		return "", err
	}
	if pkg == nil || pkg.TenantID != tenantID {
		return "", domain.ErrNotFound
	}
	if s.minioClient == nil || pkg.ArtifactPath == "" {
		return "", domain.ErrNotFound
	}

	presignedURL, err := s.minioClient.PresignedDownloadURL(ctx, pkg.ArtifactPath, 15*time.Minute, pkg.PackageName)
	if err != nil {
		return "", err
	}
	return presignedURL.String(), nil
}

func (s *Service) toResponse(ctx context.Context, pkg *domain.DownloadPackage) *dto.PackageResponse {
	downloadURL := pkg.DownloadURL
	if downloadURL == "" && s.minioClient != nil && pkg.ArtifactPath != "" && pkg.SignStatus == "signed" {
		if u, err := s.minioClient.PresignedDownloadURL(ctx, pkg.ArtifactPath, 1*time.Hour, pkg.PackageName); err == nil {
			downloadURL = u.String()
		}
	}

	status := pkg.Status
	if status == "" {
		status = "pending"
	}

	return &dto.PackageResponse{
		ID:               pkg.ID,
		TenantID:         pkg.TenantID,
		AgentProfileID:   pkg.AgentProfileID,
		DistributionMode: pkg.DistributionMode,
		Platform:         pkg.Platform,
		Arch:             pkg.Arch,
		Version:          pkg.Version,
		PackageType:      pkg.PackageType,
		PackageName:      pkg.PackageName,
		DownloadURL:      downloadURL,
		Checksum:         pkg.Checksum,
		SignStatus:       pkg.SignStatus,
		Status:           status,
		BuildStage:       pkg.BuildStage,
		BuildProgress:    pkg.BuildProgress,
		ActivationMode:   pkg.ActivationMode,
		MaxDevices:       pkg.MaxDevices,
		BoundCount:       pkg.BoundCount,
		CreatedAt:        pkg.CreatedAt,
	}
}

