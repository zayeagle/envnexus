package marketplace

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

const (
	ideExtensionItemName       = "EnvNexus IDE Sync"
	ideExtensionDefaultVersion = "0.1.0"
	ideExtensionAuthor         = "EnvNexus"
	ideExtensionDescription    = "Sync skills and rules with EnvNexus from your IDE (VSIX marketplace plugin)."
)

// SeedIDEExtension upserts the bundled Visual Studio Code extension (.vsix) as a published plugin marketplace item.
// If the file is missing, object storage is not configured, or the path is empty, it logs and returns nil.
func SeedIDEExtension(ctx context.Context, svc *Service, vsixPath string) error {
	if svc == nil {
		return nil
	}
	if svc.minioClient == nil {
		slog.Info("marketplace: skipping IDE extension seed (object storage not configured)")
		return nil
	}
	vsixPath = strings.TrimSpace(vsixPath)
	if vsixPath == "" {
		return nil
	}
	fi, err := os.Stat(vsixPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("marketplace: IDE extension .vsix not found, skipping seed", "path", vsixPath)
			return nil
		}
		return err
	}
	if fi.IsDir() {
		slog.Info("marketplace: IDE extension path is a directory, skipping seed", "path", vsixPath)
		return nil
	}
	if fi.Size() == 0 {
		slog.Info("marketplace: IDE extension .vsix is empty, skipping seed", "path", vsixPath)
		return nil
	}
	if fi.Size() > maxMarketplaceFileBytes {
		return fmt.Errorf("IDE extension .vsix exceeds max size: %d bytes", maxMarketplaceFileBytes)
	}

	f, err := os.Open(vsixPath)
	if err != nil {
		return err
	}
	defer f.Close()

	pluginType := domain.MarketplaceItemTypePlugin
	items, _, err := svc.repo.ListMarketplaceItems(ctx, &pluginType, nil, 1, 2000)
	if err != nil {
		return err
	}
	var existing *domain.MarketplaceItem
	for _, it := range items {
		if it != nil && it.Name == ideExtensionItemName {
			existing = it
			break
		}
	}

	filename := filepath.Base(vsixPath)
	if filename == "" || filename == "." {
		filename = "envnexus-sync-0.1.0.vsix"
	}
	extensionVersion, err := readVSIXVersion(vsixPath)
	if err != nil {
		slog.Warn("marketplace: failed to parse IDE extension version from .vsix, using default", "path", vsixPath, "err", err)
		extensionVersion = ideExtensionDefaultVersion
	}
	versionedName := fmt.Sprintf("envnexus-sync-%s.vsix", normalizeVersionForFilename(extensionVersion))

	if existing == nil {
		_, err := svc.CreateMarketplaceItem(ctx, CreateMarketplaceItemInput{
			Type:        domain.MarketplaceItemTypePlugin,
			Name:        ideExtensionItemName,
			Description: ideExtensionDescription,
			Version:     extensionVersion,
			Author:      ideExtensionAuthor,
			Status:      domain.MarketplaceItemStatusPublished,
			File:        f,
			FileSize:    fi.Size(),
			Filename:    versionedName,
			ContentType: "application/octet-stream",
		})
		if err != nil {
			return err
		}
		slog.Info("marketplace: seeded IDE extension plugin", "name", ideExtensionItemName, "version", extensionVersion, "file", versionedName)
		return nil
	}

	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	_, err = svc.UpdateMarketplaceItem(ctx, existing.ID, UpdateMarketplaceItemInput{
		Name:        ideExtensionItemName,
		Description: ideExtensionDescription,
		Version:     extensionVersion,
		Author:      ideExtensionAuthor,
		Status:      string(domain.MarketplaceItemStatusPublished),
		File:        f,
		FileSize:    fi.Size(),
		Filename:    versionedName,
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return err
	}
	slog.Info("marketplace: updated IDE extension plugin", "id", existing.ID, "name", ideExtensionItemName, "version", extensionVersion, "file", versionedName)
	return nil
}

func normalizeVersionForFilename(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		return "latest"
	}
	v = strings.Map(func(r rune) rune {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, v)
	v = strings.Trim(v, "._-")
	if v == "" {
		return "latest"
	}
	return v
}

func readVSIXVersion(vsixPath string) (string, error) {
	zr, err := zip.OpenReader(vsixPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, "package.json") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		body, readErr := io.ReadAll(io.LimitReader(rc, 1024*1024))
		_ = rc.Close()
		if readErr != nil {
			return "", readErr
		}
		var pkg struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal(body, &pkg); err != nil {
			continue
		}
		if strings.TrimSpace(pkg.Name) != "envnexus-sync" {
			continue
		}
		v := strings.TrimSpace(pkg.Version)
		if v == "" {
			return "", errors.New("extension version is empty in VSIX package.json")
		}
		return v, nil
	}

	return "", errors.New("envnexus-sync package.json not found in VSIX")
}
