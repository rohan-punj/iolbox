package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// darwinSyncConfig is resolved on the host. The guest keeps its normal
// /opt/iolbox stores; lifecycle-boundary API sync is the persistence bridge.
type darwinSyncConfig struct {
	NoSync    bool
	ImagesDir string
	LabsDir   string
}

var darwinUserConfigDir = os.UserConfigDir

func resolveDarwinSyncDirs(imagesOverride, labsOverride string, configDir func() (string, error)) (string, string, error) {
	if imagesOverride == "" || labsOverride == "" {
		if configDir == nil {
			configDir = darwinUserConfigDir
		}
		root, err := configDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve macOS user config directory: %w", err)
		}
		root = filepath.Join(root, "iolbox")
		if imagesOverride == "" {
			imagesOverride = filepath.Join(root, "images")
		}
		if labsOverride == "" {
			labsOverride = filepath.Join(root, "labs")
		}
	}
	return imagesOverride, labsOverride, nil
}

func defaultDarwinSyncDirs(imagesOverride, labsOverride string) (string, string, error) {
	return resolveDarwinSyncDirs(imagesOverride, labsOverride, nil)
}

func resolveDarwinSyncConfig(noSync bool, imagesOverride, labsOverride string) (darwinSyncConfig, error) {
	config := darwinSyncConfig{NoSync: noSync}
	if noSync {
		return config, nil
	}
	imagesDir, labsDir, err := defaultDarwinSyncDirs(imagesOverride, labsOverride)
	if err != nil {
		return darwinSyncConfig{}, err
	}
	config.ImagesDir = imagesDir
	config.LabsDir = labsDir
	return config, nil
}

func syncDarwinStartup(control *controlWSClient, baseURL string, config darwinSyncConfig) error {
	if config.NoSync {
		return nil
	}
	if control == nil {
		return fmt.Errorf("Darwin startup sync has no control WebSocket")
	}
	if err := ensureDirs(config.ImagesDir, config.LabsDir); err != nil {
		return err
	}
	client := &wsControlClient{ws: control}
	fs := newDarwinFolderSync(config.ImagesDir, config.LabsDir, client, newHTTPImageUploader(baseURL))
	// Rescue guest-only documents first. Existing host documents are retained,
	// then pushed below so the host copy wins an ID collision.
	if count, err := fs.syncLabsOutMissingOnly(); err != nil {
		return fmt.Errorf("Darwin startup lab rescue: %w", err)
	} else {
		logf("Darwin folder sync: rescued %d guest lab(s) into %s", count, config.LabsDir)
	}
	if count, err := fs.syncLabsIn(); err != nil {
		return fmt.Errorf("Darwin startup lab import: %w", err)
	} else {
		logf("Darwin folder sync: imported %d host lab(s) from %s", count, config.LabsDir)
	}
	if count, err := fs.syncImagesIn(); err != nil {
		return fmt.Errorf("Darwin startup image import: %w", err)
	} else {
		logf("Darwin folder sync: imported %d host image(s) from %s", count, config.ImagesDir)
	}
	return nil
}

func syncDarwinBeforeStop(control controlClient, config darwinSyncConfig) error {
	if config.NoSync {
		return nil
	}
	if control == nil {
		return fmt.Errorf("Darwin stop sync has no control WebSocket")
	}
	if err := ensureDirs(config.ImagesDir, config.LabsDir); err != nil {
		return err
	}
	fs := newDarwinFolderSync(config.ImagesDir, config.LabsDir, control, nil)
	count, err := fs.syncLabsOutStrict()
	if err != nil {
		return fmt.Errorf("Darwin stop lab export failed after %d document(s): %w", count, err)
	}
	logf("Darwin folder sync: exported %d lab(s) to %s before Lima stop", count, config.LabsDir)
	return nil
}
