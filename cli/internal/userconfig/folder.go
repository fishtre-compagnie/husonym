package userconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	husonymFolderName = ".husonym"
)

// Get or Creates the Nucleus folder that lives and stores persisted settings.
//
// 1. Checks for directory specified by env var HUSONYM_CONFIG_DIR
// 2. Checks for existence of XDG_CONFIG_HOME and append "husonym" to it, if exists
// 3. Use ~/.husonym
func GetOrCreateHusonymFolder() (string, error) {
	configDir := os.Getenv(
		"HUSONYM_CONFIG_DIR",
	) // helpful for tools such as direnv and people who want it somewhere interesting
	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME") // linux users expect this to be respected

	var fullName string
	if configDir != "" {
		if strings.HasPrefix(configDir, "~/") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			configDir = filepath.Join(homeDir, configDir[2:])
		}
		fullName = configDir
	} else if xdgConfigHome != "" || runtime.GOOS == "linux" || strings.Contains(runtime.GOOS, "bsd") {
		var baseDir string
		if xdgConfigHome != "" {
			baseDir = xdgConfigHome
		} else {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			baseDir = filepath.Join(homeDir, ".config")
			if err := ensureDirectoryExists(baseDir); err != nil {
				return "", err
			}
		}
		fullName = filepath.Join(baseDir, husonymFolderName[1:])
	} else {
		dirname, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		fullName = filepath.Join(dirname, husonymFolderName)
	}

	if err := ensureDirectoryExists(fullName); err != nil {
		return "", err
	}
	return fullName, nil
}

// ensureDirectoryExists checks for directory existence and tries to create it if it does not exist.
func ensureDirectoryExists(dirName string) error {
	//nolint:gosec // dirName is the CLI's own config directory derived from the user home/env
	_, err := os.Stat(dirName)
	if os.IsNotExist(err) {
		//nolint:gosec // dirName is the CLI's own config directory derived from the user home/env
		err = os.Mkdir(dirName, 0755)
		if err != nil {
			if os.IsExist(err) {
				return nil
			}
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}
