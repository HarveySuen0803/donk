package src

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type OSSConfig struct {
	Name      string `json:"name"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket"`
	Endpoint  string `json:"endpoint"`
}

type OSSClient struct {
	cfg    OSSConfig
	bucket *oss.Bucket
}

var errOSSObjectOrPrefixNotFound = errors.New("remote object or directory was not found in OSS")

func NewOSSClient(cfg OSSConfig) (*OSSClient, error) {
	if cfg.Name != "" && cfg.Name != "aliyun-oss" {
		return nil, fmt.Errorf("failed to initialize OSS client because the provider is not supported: %s", cfg.Name)
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.Endpoint == "" {
		return nil, errors.New("failed to initialize OSS client because access key, secret key, and endpoint are required")
	}
	if cfg.Bucket == "" || cfg.Bucket == "/" {
		return nil, errors.New("failed to initialize OSS client because bucket is required")
	}

	endpoint := normalizeEndpoint(cfg.Endpoint)
	client, err := oss.New(endpoint, cfg.AccessKey, cfg.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create OSS SDK client: %w", err)
	}
	bucketName := strings.Trim(cfg.Bucket, "/")
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to access OSS bucket: %w", err)
	}

	return &OSSClient{
		cfg:    cfg,
		bucket: bucket,
	}, nil
}

func (o *OSSClient) Pull(src string, dst string) error {
	_, key, err := o.parseUri(src)
	if err != nil {
		return err
	}

	if key == "" {
		return errors.New("pull failed because the OSS path is invalid and the object key is missing")
	}

	// Single object path.
	exists, err := o.bucket.IsObjectExist(key)
	if err != nil {
		return fmt.Errorf("pull failed while checking whether the OSS object exists: %w", err)
	}
	if exists {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return o.bucket.GetObjectToFile(key, dst)
	}

	// Prefix path.
	prefix := key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	marker := ""
	found := false
	for {
		result, err := o.bucket.ListObjectsV2(
			oss.Prefix(prefix),
			oss.ContinuationToken(marker),
		)
		if err != nil {
			return fmt.Errorf("pull failed while listing OSS objects under prefix %s: %w", prefix, err)
		}
		for _, obj := range result.Objects {
			found = true
			rel := strings.TrimPrefix(obj.Key, prefix)
			localFile := filepath.Join(dst, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(localFile), 0o755); err != nil {
				return err
			}
			if err := o.bucket.GetObjectToFile(obj.Key, localFile); err != nil {
				return fmt.Errorf("pull failed while downloading OSS object %s: %w", obj.Key, err)
			}
		}
		if !result.IsTruncated {
			break
		}
		marker = result.NextContinuationToken
	}

	if !found {
		return fmt.Errorf("%w. Source path: %s", errOSSObjectOrPrefixNotFound, src)
	}
	return nil
}

func (o *OSSClient) Push(src string, dst string) error {
	_, key, err := o.parseUri(dst)
	if err != nil {
		return err
	}
	if key == "" {
		return errors.New("push failed because the OSS path is invalid and the object key is missing")
	}

	stat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("push failed because the local source path is unavailable: %s", src)
	}
	if stat.IsDir() {
		if err := o.deletePrefixContents(key); err != nil {
			return err
		}
		return filepath.Walk(src, func(path string, fileInfo os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if fileInfo.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			objectKey := joinOSSKey(key, filepath.ToSlash(rel))
			if err := o.bucket.PutObjectFromFile(objectKey, path); err != nil {
				return fmt.Errorf("push failed while uploading OSS object %s: %w", objectKey, err)
			}
			return nil
		})
	} else {
		if err := o.bucket.PutObjectFromFile(key, src); err != nil {
			return fmt.Errorf("push failed while uploading OSS object %s: %w", key, err)
		}
		return nil
	}
}

func (o *OSSClient) ReadObject(src string) ([]byte, error) {
	_, key, err := o.parseUri(src)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, errors.New("read failed because the OSS path is invalid and the object key is missing")
	}

	exists, err := o.bucket.IsObjectExist(key)
	if err != nil {
		return nil, fmt.Errorf("read failed while checking whether the OSS object exists: %w", err)
	}
	if !exists {
		return nil, os.ErrNotExist
	}

	reader, err := o.bucket.GetObject(key)
	if err != nil {
		return nil, fmt.Errorf("read failed while opening OSS object %s: %w", key, err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read failed while reading content from OSS object %s: %w", key, err)
	}
	return content, nil
}

func (o *OSSClient) WriteObject(dest string, content []byte) error {
	_, key, err := o.parseUri(dest)
	if err != nil {
		return err
	}
	if key == "" {
		return errors.New("write failed because the OSS path is invalid and the object key is missing")
	}
	if err := o.bucket.PutObject(key, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write failed while uploading OSS object %s: %w", key, err)
	}
	return nil
}

func (o *OSSClient) deletePrefixContents(base string) error {
	marker := ""
	for {
		result, err := o.bucket.ListObjectsV2(
			oss.Prefix(base),
			oss.ContinuationToken(marker),
		)
		if err != nil {
			return fmt.Errorf("push failed while listing existing OSS objects under prefix %s before overwrite: %w", base, err)
		}
		for _, obj := range result.Objects {
			if obj.Key != base && !strings.HasPrefix(obj.Key, base+"/") {
				continue
			}
			if err := o.bucket.DeleteObject(obj.Key); err != nil {
				return fmt.Errorf("push failed while deleting stale OSS object %s: %w", obj.Key, err)
			}
		}
		if !result.IsTruncated {
			break
		}
		marker = result.NextContinuationToken
	}
	return nil
}

func (o *OSSClient) parseUri(raw string) (string, string, error) {
	if !strings.HasPrefix(raw, "oss://") {
		return "", "", fmt.Errorf("invalid OSS path because the scheme is not oss://. Path: %s", raw)
	}
	withoutScheme := strings.TrimPrefix(raw, "oss://")
	parts := strings.SplitN(withoutScheme, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", fmt.Errorf("invalid OSS path because the bucket segment is missing. Path: %s", raw)
	}

	pathBucket := parts[0]
	key := ""
	if len(parts) == 2 {
		key = strings.TrimPrefix(parts[1], "/")
	}

	cfgBucket := strings.Trim(o.cfg.Bucket, "/")
	if cfgBucket != "" && cfgBucket != pathBucket {
		return "", "", fmt.Errorf("invalid OSS path because the bucket does not match the configured bucket. Path bucket: %s. Config bucket: %s", pathBucket, cfgBucket)
	}
	return cfgBucket, key, nil
}

func normalizeEndpoint(endpoint string) string {
	if strings.Contains(endpoint, "://") {
		return endpoint
	}
	if strings.HasPrefix(endpoint, "oss-") {
		return "https://" + endpoint + ".aliyuncs.com"
	}
	return "https://" + endpoint
}

func joinOSSKey(base, rel string) string {
	base = strings.TrimSuffix(base, "/")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return base
	}
	if base == "" {
		return rel
	}
	return base + "/" + rel
}
