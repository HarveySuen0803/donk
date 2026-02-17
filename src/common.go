package src

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ConfigEntry struct {
	Name string   `json:"name"`
	OSS  string   `json:"oss"`
	Link string   `json:"link"`
	Cmd  []string `json:"cmd"`
}

type Settings struct {
	Version int           `json:"version"`
	Cfg     []ConfigEntry `json:"cfg"`
	Lib     []ConfigEntry `json:"lib"`
	OSS     OSSConfig     `json:"oss"`
}

const (
	defaultCfgOSSPrefix = "donk/cfg"
	defaultLibOSSPrefix = "donk/lib"
)

type Context struct {
	Dir      string
	Settings Settings
}

func LoadContext(dir string) (Context, error) {
	var context Context
	path := filepath.Join(dir, "settings.json")
	settings, err := LoadSettings(path)
	if err != nil {
		return context, err
	}
	context.Dir = dir
	context.Settings = settings
	return context, nil
}

func LoadSettings(path string) (Settings, error) {
	var settings Settings
	content, err := os.ReadFile(path)
	if err != nil {
		return settings, err
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		return settings, fmt.Errorf("failed to parse settings file: %w", err)
	}
	if err := settings.normalizeEntryOSS(); err != nil {
		return settings, err
	}
	return settings, nil
}

func (s *Settings) normalizeEntryOSS() error {
	bucket := strings.Trim(s.OSS.Bucket, "/")
	for idx := range s.Cfg {
		if strings.TrimSpace(s.Cfg[idx].OSS) != "" {
			continue
		}
		if bucket == "" {
			return fmt.Errorf("cfg entry is missing oss and cannot use default because oss.bucket is empty. Entry name: %s", s.Cfg[idx].Name)
		}
		s.Cfg[idx].OSS = fmt.Sprintf("oss://%s/%s/%s", bucket, defaultCfgOSSPrefix, s.Cfg[idx].Name)
	}
	for idx := range s.Lib {
		if strings.TrimSpace(s.Lib[idx].OSS) != "" {
			continue
		}
		if bucket == "" {
			return fmt.Errorf("lib entry is missing oss and cannot use default because oss.bucket is empty. Entry name: %s", s.Lib[idx].Name)
		}
		s.Lib[idx].OSS = fmt.Sprintf("oss://%s/%s/%s", bucket, defaultLibOSSPrefix, s.Lib[idx].Name)
	}
	return nil
}

func findEntry(entries []ConfigEntry, name string) (ConfigEntry, error) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, nil
		}
	}
	return ConfigEntry{}, fmt.Errorf("configuration entry was not found for name: %s", name)
}

func expandPath(input string) (string, error) {
	if strings.HasPrefix(input, "~/") ||
		strings.HasPrefix(input, "$HOME/") ||
		input == "~" ||
		input == "$HOME" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		switch input {
		case "~", "$HOME":
			return home, nil
		}
		if strings.HasPrefix(input, "~/") {
			return filepath.Join(home, strings.TrimPrefix(input, "~/")), nil
		}
		return filepath.Join(home, strings.TrimPrefix(input, "$HOME/")), nil
	}
	return input, nil
}

func ensureSymlink(target, linkPath string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	absLink, err := filepath.Abs(linkPath)
	if err != nil {
		return err
	}

	info, err := os.Lstat(absLink)
	switch {
	case errorsIsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(absLink), 0o755); err != nil {
			return err
		}
		return os.Symlink(absTarget, absLink)
	case err != nil:
		return err
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("cannot create symbolic link because the link path is occupied by a file or directory: %s", absLink)
	}

	currentTarget, err := os.Readlink(absLink)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(currentTarget) {
		currentTarget = filepath.Join(filepath.Dir(absLink), currentTarget)
	}
	currentTargetAbs, err := filepath.Abs(currentTarget)
	if err != nil {
		return err
	}
	if currentTargetAbs == absTarget {
		return nil
	}

	if err := os.Remove(absLink); err != nil {
		return err
	}
	return os.Symlink(absTarget, absLink)
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}

func pullSource(settings Settings, src string, dst string) error {
	if strings.HasPrefix(src, "oss://") {
		ossClient, err := NewOSSClient(settings.OSS)
		if err != nil {
			return err
		}
		return ossClient.Pull(src, dst)
	} else {
		return fmt.Errorf("unsupported data source. Only oss paths are currently supported: %s", src)
	}
}

func pushSource(settings Settings, src string, dst string) error {
	if strings.HasPrefix(dst, "oss://") {
		ossClient, err := NewOSSClient(settings.OSS)
		if err != nil {
			return err
		}
		return ossClient.Push(src, dst)
	} else {
		return fmt.Errorf("unsupported data destination. Only oss paths are currently supported: %s", dst)
	}
}

func runCommands(commands []string) error {
	for idx, command := range commands {
		if strings.TrimSpace(command) == "" {
			continue
		}
		cmd := exec.Command("sh", "-c", command)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to execute configured command at index %d. Command: %q. Details: %w", idx, command, err)
		}
	}
	return nil
}
