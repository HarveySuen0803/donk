package src

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const cfgUsageText = "usage: donk cfg pull <name> | donk cfg push <name> | donk cfg init <name>"

const (
	cfgManifestVersion   = 1
	cfgManifestAlgorithm = "sha256"
)

type CfgCmd struct {
	Context Context
}

type CfgManifest struct {
	Version   int                         `json:"version"`
	Algorithm string                      `json:"algorithm"`
	Entries   map[string]CfgManifestEntry `json:"entries"`
}

type CfgManifestEntry struct {
	Root           string            `json:"root"`
	Revision       int64             `json:"revision"`
	UpdatedAt      string            `json:"updated_at"`
	UpdatedBy      string            `json:"updated_by"`
	Files          []CfgManifestFile `json:"files"`
	ManifestSHA256 string            `json:"manifest_sha256"`
}

type CfgManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func CreateCfgCmd(context Context) CfgCmd {
	return CfgCmd{Context: context}
}

func (c CfgCmd) Run(args []string) error {
	switch {
	case len(args) == 3 && args[0] == "cfg" && args[1] == "pull":
		return c.Pull(args[2])
	case len(args) == 3 && args[0] == "cfg" && args[1] == "push":
		return c.Push(args[2])
	case len(args) == 3 && args[0] == "cfg" && args[1] == "init":
		return c.Init(args[2])
	default:
		return fmt.Errorf("invalid command arguments. %s", cfgUsageText)
	}
}

func (c CfgCmd) Init(name string) error {
	entry, err := findEntry(c.Context.Settings.Cfg, name)
	if err != nil {
		return err
	}

	localCfgDir := c.buildLocalCfgDir(name)
	linkPlans, err := buildSymlinkPlans(entry.Name, entry.Link, localCfgDir)
	if err != nil {
		return err
	}
	primaryLinkPath, err := findPrimaryLinkPath(entry.Link, linkPlans)
	if err != nil {
		return err
	}
	absLocalCfgDir, err := filepath.Abs(localCfgDir)
	if err != nil {
		return err
	}

	linkInfo, err := os.Lstat(primaryLinkPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("configuration init failed because the configured link path does not exist: %s", primaryLinkPath)
		}
		return err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		currentTarget, err := os.Readlink(primaryLinkPath)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(currentTarget) {
			currentTarget = filepath.Join(filepath.Dir(primaryLinkPath), currentTarget)
		}
		absCurrentTarget, err := filepath.Abs(currentTarget)
		if err != nil {
			return err
		}
		if absCurrentTarget == absLocalCfgDir {
			return fmt.Errorf("configuration init was skipped because the link path is already initialized: %s", primaryLinkPath)
		}
		return fmt.Errorf("configuration init failed because the configured link path is an unexpected symbolic link: %s", primaryLinkPath)
	}
	if !linkInfo.IsDir() {
		return fmt.Errorf("configuration init failed because the configured link path is not a directory: %s", primaryLinkPath)
	}

	if _, err := os.Lstat(localCfgDir); err == nil {
		return fmt.Errorf("configuration init failed because the local cfg directory already exists: %s", localCfgDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := c.copyDirForCfgInit(primaryLinkPath, localCfgDir); err != nil {
		return err
	}
	if err := c.Push(name); err != nil {
		return err
	}
	if err := os.RemoveAll(primaryLinkPath); err != nil {
		return fmt.Errorf("configuration init failed while removing original directory: %w", err)
	}
	if err := ensureSymlinks(linkPlans); err != nil {
		return err
	}
	if err := runCommands(entry.Cmd); err != nil {
		return err
	}

	fmt.Printf("configuration init completed successfully for: %s\n", name)
	return nil
}

func (c CfgCmd) Pull(name string) error {
	entry, err := findEntry(c.Context.Settings.Cfg, name)
	if err != nil {
		return err
	}

	remoteManifest, err := c.loadRemoteCfgManifest()
	if err != nil {
		return err
	}
	localManifestPath := c.buildLocalCfgManifestPath()
	localManifest, err := c.loadLocalCfgManifest(localManifestPath)
	if err != nil {
		return err
	}

	remoteManifestEntry, isRemoteManifestExists := remoteManifest.Entries[name]
	localManifestEntry, isLocalManifestExists := localManifest.Entries[name]
	remoteRevision := int64(0)
	if isRemoteManifestExists {
		remoteRevision = remoteManifestEntry.Revision
	}
	localRevision := int64(0)
	if isLocalManifestExists {
		localRevision = localManifestEntry.Revision
	}

	localCfgDir := c.buildLocalCfgDir(name)

	doPull := func() error {
		tempLocalCfgDir := localCfgDir + ".tmp"
		if err := os.RemoveAll(tempLocalCfgDir); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(localCfgDir), 0o755); err != nil {
			return err
		}
		if err := c.pullSource(entry.OSS, tempLocalCfgDir); err != nil {
			_ = os.RemoveAll(tempLocalCfgDir)
			return err
		}
		if err := c.renameDir(localCfgDir, tempLocalCfgDir); err != nil {
			_ = os.RemoveAll(tempLocalCfgDir)
			return err
		}

		if localManifest.Entries == nil {
			localManifest.Entries = map[string]CfgManifestEntry{}
		}
		localManifest.Entries[name] = remoteManifestEntry
		if err := c.saveLocalCfgManifest(c.buildLocalCfgManifestPath(), localManifest); err != nil {
			return err
		}

		symlinkPlans, err := buildSymlinkPlans(entry.Name, entry.Link, localCfgDir)
		if err != nil {
			return err
		}
		if err := ensureSymlinks(symlinkPlans); err != nil {
			return err
		}
		if err := runCommands(entry.Cmd); err != nil {
			return err
		}

		fmt.Printf("configuration pull completed successfully for: %s\n", name)

		return nil
	}

	switch {
	case localRevision > remoteRevision:
		return fmt.Errorf("configuration pull cannot continue because the local revision is newer than the remote revision. Local revision: %d. Remote revision: %d. Please run cfg push first", localRevision, remoteRevision)
	case localRevision == remoteRevision:
		if !isRemoteManifestExists {
			localFiles, err := c.buildCfgFileSnapshot(localCfgDir)
			if err != nil {
				return err
			}
			if len(localFiles) == 0 {
				fmt.Printf("configuration pull was skipped because no remote manifest entry exists and no local files were found for: %s\n", name)
				return nil
			}
			return fmt.Errorf("configuration pull failed because local files exist while the remote manifest entry is missing at the same revision. Revision: %d. Name: %s", localRevision, name)
		}
		if _, err := os.Stat(localCfgDir); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return doPull()
			} else {
				return err
			}
		}
		isEqual, err := c.isLocalCfgEqualToManifest(localCfgDir, remoteManifestEntry)
		if err != nil {
			return err
		}
		if isEqual {
			fmt.Printf("configuration pull was skipped because local and remote content are already identical for: %s\n", name)
			return nil
		} else {
			return fmt.Errorf("configuration pull failed because local content differs from the remote manifest at the same revision. Revision: %d. Name: %s", localRevision, name)
		}
	case localRevision < remoteRevision:
		if isRemoteManifestExists {
			return doPull()
		} else {
			return fmt.Errorf("configuration pull failed because the remote manifest entry is missing. Name: %s. Remote revision: %d", name, remoteRevision)
		}
	}

	return nil
}

func (c CfgCmd) Push(name string) error {
	entry, err := findEntry(c.Context.Settings.Cfg, name)
	if err != nil {
		return err
	}

	localCfgDir := c.buildLocalCfgDir(name)
	if _, err := os.Stat(localCfgDir); err != nil {
		return fmt.Errorf("configuration push failed because the local source directory does not exist: %s", localCfgDir)
	}

	remoteManifest, err := c.loadRemoteCfgManifest()
	if err != nil {
		return err
	}
	localManifest, err := c.loadLocalCfgManifest(c.buildLocalCfgManifestPath())
	if err != nil {
		return err
	}

	remoteEntry, remoteExists := remoteManifest.Entries[name]
	localEntry, localExists := localManifest.Entries[name]
	remoteRevision := int64(0)
	if remoteExists {
		remoteRevision = remoteEntry.Revision
	}
	localRevision := int64(0)
	if localExists {
		localRevision = localEntry.Revision
	}

	if localRevision < remoteRevision {
		return fmt.Errorf("configuration push cannot continue because the local revision is behind the remote revision. Local revision: %d. Remote revision: %d. Please run donk cfg pull %s first", localRevision, remoteRevision, name)
	}

	files, err := c.buildCfgFileSnapshot(localCfgDir)
	if err != nil {
		return err
	}
	if remoteExists && c.isCfgManifestFilesEqual(files, remoteEntry.Files) {
		fmt.Printf("configuration push was skipped because local and remote content are already identical for: %s\n", name)
		return nil
	}

	newRevision := localRevision + 1
	newEntry, err := c.buildManifestEntry(localCfgDir, newRevision, files)
	if err != nil {
		return err
	}

	if err := c.pushSource(localCfgDir, entry.OSS); err != nil {
		return err
	}

	if localManifest.Entries == nil {
		localManifest.Entries = map[string]CfgManifestEntry{}
	}
	if remoteManifest.Entries == nil {
		remoteManifest.Entries = map[string]CfgManifestEntry{}
	}
	localManifest.Entries[name] = newEntry
	remoteManifest.Entries[name] = newEntry

	if err := c.saveRemoteCfgManifest(remoteManifest); err != nil {
		return err
	}
	if err := c.saveLocalCfgManifest(c.buildLocalCfgManifestPath(), localManifest); err != nil {
		return err
	}

	fmt.Printf("configuration push completed successfully for: %s\n", name)
	return nil
}

func (c CfgCmd) buildLocalCfgDir(name string) string {
	return filepath.Join(c.Context.Dir, "cfg", name)
}

func (c CfgCmd) buildLocalCfgManifestPath() string {
	return filepath.Join(c.Context.Dir, "cfg", "manifest.json")
}

func (c CfgCmd) buildRemoteManifestPath() string {
	bucket := strings.Trim(c.Context.Settings.OSS.Bucket, "/")
	return fmt.Sprintf("oss://%s/donk/cfg/manifest.json", bucket)
}

func (c CfgCmd) loadLocalCfgManifest(path string) (CfgManifest, error) {
	manifest := c.defaultCfgManifest()
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return manifest, nil
		}
		return CfgManifest{}, err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return manifest, nil
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return CfgManifest{}, fmt.Errorf("failed to parse configuration manifest file: %w", err)
	}
	if manifest.Entries == nil {
		manifest.Entries = map[string]CfgManifestEntry{}
	}
	if manifest.Version == 0 {
		manifest.Version = cfgManifestVersion
	}
	if manifest.Algorithm == "" {
		manifest.Algorithm = cfgManifestAlgorithm
	}
	return manifest, nil
}

func (c CfgCmd) saveLocalCfgManifest(path string, manifest CfgManifest) error {
	if manifest.Version == 0 {
		manifest.Version = cfgManifestVersion
	}
	if manifest.Algorithm == "" {
		manifest.Algorithm = cfgManifestAlgorithm
	}
	if manifest.Entries == nil {
		manifest.Entries = map[string]CfgManifestEntry{}
	}

	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

func (c CfgCmd) loadRemoteCfgManifest() (CfgManifest, error) {
	tmpFile, err := os.CreateTemp("", "donk-cfg-remote-manifest-*.json")
	if err != nil {
		return CfgManifest{}, err
	}
	dst := tmpFile.Name()
	_ = tmpFile.Close()
	_ = os.Remove(dst)
	defer os.Remove(dst)

	src := c.buildRemoteManifestPath()
	if err := c.pullSource(src, dst); err != nil {
		if c.isOSSNotFoundErr(err) {
			return c.defaultCfgManifest(), nil
		}
		return CfgManifest{}, err
	}

	return c.loadLocalCfgManifest(dst)
}

func (c CfgCmd) saveRemoteCfgManifest(manifest CfgManifest) error {
	tmpFile, err := os.CreateTemp("", "donk-cfg-remote-manifest-*.json")
	if err != nil {
		return err
	}
	src := tmpFile.Name()
	_ = tmpFile.Close()
	defer os.Remove(src)

	// todo: refine this
	if err := c.saveLocalCfgManifest(src, manifest); err != nil {
		return err
	}

	dst := c.buildRemoteManifestPath()

	return c.pushSource(src, dst)
}

func (c CfgCmd) defaultCfgManifest() CfgManifest {
	return CfgManifest{
		Version:   cfgManifestVersion,
		Algorithm: cfgManifestAlgorithm,
		Entries:   map[string]CfgManifestEntry{},
	}
}

func (c CfgCmd) isOSSNotFoundErr(err error) bool {
	return errors.Is(err, errOSSObjectOrPrefixNotFound)
}

func (c CfgCmd) buildCfgFileSnapshot(root string) ([]CfgManifestFile, error) {
	files := make([]CfgManifestFile, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("configuration snapshot failed because a non regular file was found: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sha256, err := c.fileSHA256(path)
		if err != nil {
			return err
		}
		files = append(files, CfgManifestFile{
			Path:   filepath.ToSlash(rel),
			Size:   info.Size(),
			SHA256: sha256,
		})
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []CfgManifestFile{}, nil
		}
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func (c CfgCmd) fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c CfgCmd) manifestFilesSHA256(files []CfgManifestFile) (string, error) {
	content, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:]), nil
}

func (c CfgCmd) buildManifestEntry(root string, revision int64, files []CfgManifestFile) (CfgManifestEntry, error) {
	manifestHash, err := c.manifestFilesSHA256(files)
	if err != nil {
		return CfgManifestEntry{}, err
	}
	updatedBy := os.Getenv("USER")
	host, _ := os.Hostname()
	if updatedBy == "" {
		updatedBy = "unknown"
	}
	if host == "" {
		host = "unknown"
	}
	return CfgManifestEntry{
		Root:           root,
		Revision:       revision,
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
		UpdatedBy:      updatedBy + "@" + host,
		Files:          files,
		ManifestSHA256: manifestHash,
	}, nil
}

func (c CfgCmd) isCfgManifestFilesEqual(a []CfgManifestFile, b []CfgManifestFile) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]CfgManifestFile(nil), a...)
	bb := append([]CfgManifestFile(nil), b...)
	sort.Slice(aa, func(i, j int) bool { return aa[i].Path < aa[j].Path })
	sort.Slice(bb, func(i, j int) bool { return bb[i].Path < bb[j].Path })
	for i := range aa {
		if aa[i].Path != bb[i].Path || aa[i].Size != bb[i].Size || aa[i].SHA256 != bb[i].SHA256 {
			return false
		}
	}
	return true
}

func (c CfgCmd) isLocalCfgEqualToManifest(root string, entry CfgManifestEntry) (bool, error) {
	files, err := c.buildCfgFileSnapshot(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return len(entry.Files) == 0, nil
		}
		return false, err
	}
	return c.isCfgManifestFilesEqual(files, entry.Files), nil
}

func (c CfgCmd) renameDir(dst string, tmp string) error {
	bak := dst + ".bak"
	_ = os.RemoveAll(bak)

	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, bak); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := os.Rename(tmp, dst); err != nil {
		if _, bkErr := os.Stat(bak); bkErr == nil {
			_ = os.Rename(bak, dst)
		}
		return err
	}
	_ = os.RemoveAll(bak)

	return nil
}

func (c CfgCmd) copyDirForCfgInit(src string, dst string) error {
	tmpDst := dst + ".tmp"
	if err := os.RemoveAll(tmpDst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(tmpDst, 0o755); err != nil {
		return err
	}
	copyErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		targetPath := filepath.Join(tmpDst, rel)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("configuration init failed because a non regular file was found in source directory: %s", path)
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}

		dstFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(dstFile, srcFile); err != nil {
			_ = srcFile.Close()
			_ = dstFile.Close()
			return err
		}
		if err := srcFile.Close(); err != nil {
			_ = dstFile.Close()
			return err
		}
		if err := dstFile.Close(); err != nil {
			return err
		}
		return nil
	})
	if copyErr != nil {
		_ = os.RemoveAll(tmpDst)
		return copyErr
	}
	if err := os.Rename(tmpDst, dst); err != nil {
		_ = os.RemoveAll(tmpDst)
		return err
	}
	return nil
}

func (c CfgCmd) pushSource(src string, dst string) error {
	return pushSource(c.Context.Settings, src, dst)
}

func (c CfgCmd) pullSource(src string, dst string) error {
	return pullSource(c.Context.Settings, src, dst)
}
