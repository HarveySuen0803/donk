package src

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const initUsageText = "usage: donk init"

type InitCmd struct {
	DefaultSettings []byte
}

func CreateInitCmd(embeddedFiles embed.FS) (InitCmd, error) {
	var initCmd InitCmd
	defaultSettings, err := embeddedFiles.ReadFile("settings.json")
	if err != nil {
		return initCmd, err
	}
	initCmd.DefaultSettings = defaultSettings
	return initCmd, nil
}

func (i InitCmd) Run(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "init":
		return i.runInit()
	default:
		return fmt.Errorf("invalid command arguments. %s", initUsageText)
	}
}

func (i InitCmd) runInit() error {
	donkDir, created, err := i.Ensure()
	if err != nil {
		return err
	}

	settingsPath := filepath.Join(donkDir, "settings.json")
	if created {
		fmt.Printf("initialization completed. settings file path: %s\n", settingsPath)
		return nil
	}

	fmt.Printf("initialization skipped. settings file already exists at: %s\n", settingsPath)

	return nil
}

func (i InitCmd) Ensure() (string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}

	donkDir := filepath.Join(home, ".donk")
	if err := os.MkdirAll(donkDir, 0o755); err != nil {
		return "", false, err
	}

	settingsPath := filepath.Join(donkDir, "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		return donkDir, false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}

	if len(i.DefaultSettings) == 0 {
		return "", false, errors.New("initialization failed because the embedded default settings file is empty")
	}
	if err := os.WriteFile(settingsPath, i.DefaultSettings, 0o644); err != nil {
		return "", false, err
	}
	return donkDir, true, nil
}

func (i InitCmd) LoadContext() (Context, error) {
	var context Context
	dir, _, err := i.Ensure()
	if err != nil {
		return context, err
	}
	path := filepath.Join(dir, "settings.json")
	settings, err := LoadSettings(path)
	if err != nil {
		return context, err
	}
	context.Dir = dir
	context.Settings = settings
	return context, nil
}
