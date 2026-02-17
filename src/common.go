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
	Name string     `json:"name"`
	OSS  string     `json:"oss"`
	Link LinkConfig `json:"link"`
	Cmd  []string   `json:"cmd"`
}

type Settings struct {
	Version int           `json:"version"`
	Cfg     []ConfigEntry `json:"cfg"`
	Lib     []ConfigEntry `json:"lib"`
	OSS     OSSConfig     `json:"oss"`
}

type LinkConfig []string

type SymlinkPlan struct {
	src  string
	link string
}

func (l *LinkConfig) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*l = LinkConfig{single}
		return nil
	}

	var multi []string
	if err := json.Unmarshal(data, &multi); err == nil {
		*l = LinkConfig(multi)
		return nil
	}

	return fmt.Errorf("link must be a string or an array of strings")
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

func ensureSymlink(src string, link string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	linkAbs, err := filepath.Abs(link)
	if err != nil {
		return err
	}

	info, err := os.Lstat(linkAbs)
	switch {
	case errorsIsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(linkAbs), 0o755); err != nil {
			return err
		}
		return os.Symlink(srcAbs, linkAbs)
	case err != nil:
		return err
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("cannot create symbolic link because the link path is occupied by a file or directory: %s", linkAbs)
	}

	currSrc, err := os.Readlink(linkAbs)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(currSrc) {
		currSrc = filepath.Join(filepath.Dir(linkAbs), currSrc)
	}
	currSrcAbs, err := filepath.Abs(currSrc)
	if err != nil {
		return err
	}
	if currSrcAbs == srcAbs {
		return nil
	}

	return fmt.Errorf("cannot create symbolic link because it points to a different target. Link: %s. Current target: %s. Expected target: %s", linkAbs, currSrcAbs, srcAbs)
}

func ensureSymlinks(plans []SymlinkPlan) error {
	for _, plan := range plans {
		if err := ensureSymlink(plan.src, plan.link); err != nil {
			return err
		}
	}
	return nil
}

func findPrimaryLinkPath(rawLinks LinkConfig, plans []SymlinkPlan) (string, error) {
	plainIndexes := make([]int, 0)
	for idx, raw := range rawLinks {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if !strings.Contains(raw, "->") {
			plainIndexes = append(plainIndexes, idx)
		}
	}
	if len(plainIndexes) == 0 {
		return "", fmt.Errorf("no primary link path was found, please provide one plain link path without \"->\"")
	}
	if len(plainIndexes) > 1 {
		return "", fmt.Errorf("multiple primary link paths were found, please keep exactly one plain link path without \"->\"")
	}
	idx := plainIndexes[0]
	if idx < 0 || idx >= len(plans) {
		return "", fmt.Errorf("the link configuration is invalid")
	}
	return plans[idx].link, nil
}

func buildSymlinkPlans(entryName string, rawLinks LinkConfig, defaultSrc string) ([]SymlinkPlan, error) {
	if len(rawLinks) == 0 {
		return nil, fmt.Errorf("entry is missing link configuration. Entry name: %s", entryName)
	}

	plans := make([]SymlinkPlan, 0, len(rawLinks))
	seen := map[string]string{}
	for _, rawLink := range rawLinks {
		trimmed := strings.TrimSpace(rawLink)
		if trimmed == "" {
			return nil, fmt.Errorf("link item cannot be empty. Entry name: %s", entryName)
		}

		src := ""
		link := ""
		if strings.Contains(trimmed, "->") {
			parts := strings.SplitN(trimmed, "->", 2)
			link = strings.TrimSpace(parts[0])
			src = strings.TrimSpace(parts[1])
			if src == "" || link == "" {
				return nil, fmt.Errorf("invalid link mapping. Expected \"<link> -> <src>\", got: %s", trimmed)
			}
		} else {
			src = defaultSrc
			link = trimmed
		}

		srcPath, err := expandPath(src)
		if err != nil {
			return nil, err
		}
		linkPath, err := expandPath(link)
		if err != nil {
			return nil, err
		}
		absSrcPath, err := filepath.Abs(srcPath)
		if err != nil {
			return nil, err
		}
		absLinkPath, err := filepath.Abs(linkPath)
		if err != nil {
			return nil, err
		}

		if existingSrc, exists := seen[absLinkPath]; exists && existingSrc != absSrcPath {
			return nil, fmt.Errorf("link conflict detected because one link path maps to different sources. Link: %s. Source A: %s. Source B: %s", absLinkPath, existingSrc, absSrcPath)
		}
		seen[absLinkPath] = absSrcPath
		plans = append(plans, SymlinkPlan{
			src:  absSrcPath,
			link: absLinkPath,
		})
	}

	return plans, nil
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
