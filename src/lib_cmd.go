package src

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const libUsageText = "usage: donk lib pull <name>"

type LibCmd struct {
	Context Context
}

func CreateLibCmd(context Context) LibCmd {
	return LibCmd{context}
}

func (l LibCmd) Run(args []string) error {
	switch {
	case len(args) == 3 && args[0] == "lib" && args[1] == "pull":
		return l.Pull(args[2])
	default:
		return fmt.Errorf("invalid command arguments. %s", libUsageText)
	}
}

func (l LibCmd) Pull(name string) error {
	entry, err := findEntry(l.Context.Settings.Lib, name)
	if err != nil {
		return err
	}

	localLibDir := filepath.Join(l.Context.Dir, "lib", name)
	symlinkPlans, err := buildSymlinkPlans(entry.Name, entry.Link, localLibDir)
	if err != nil {
		return fmt.Errorf("library pull failed because %w", err)
	}

	if _, err := os.Lstat(localLibDir); err == nil {
		return fmt.Errorf("library pull failed because the local library directory already exists: %s", localLibDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	for _, plan := range symlinkPlans {
		if _, err := os.Lstat(plan.link); err == nil {
			return fmt.Errorf("library pull failed because the link path already exists: %s", plan.link)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(localLibDir), 0o755); err != nil {
		return err
	}
	if err := pullSource(l.Context.Settings, entry.OSS, localLibDir); err != nil {
		return err
	}

	if err := ensureSymlinks(symlinkPlans); err != nil {
		return err
	}

	fmt.Printf("library pull completed successfully for: %s\n", name)
	return nil
}
