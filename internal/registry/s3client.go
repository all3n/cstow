package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/all3n/cstow/internal/config"
)

// Manifest describes a package version's metadata
type Manifest struct {
	Name      string     `toml:"name"`
	Version   string     `toml:"version"`
	Std       string     `toml:"std"`
	License   string     `toml:"license"`
	Artifacts []Artifact `toml:"artifact"`
}

// Artifact is one pre-compiled binary artifact
type Artifact struct {
	ABITag    string   `toml:"abi_tag"`
	BuildType string   `toml:"build_type,omitempty"`
	HashID    string   `toml:"hash_id,omitempty"`
	BuildTags []string `toml:"build_tags,omitempty"`
	SHA256    string   `toml:"sha256"`
	Size      int64    `toml:"size"`
	URL       string   `toml:"url,omitempty"`
}

// SelectArtifact chooses the best matching artifact from a manifest.
// When buildType is empty, only legacy untyped artifacts are eligible.
// When buildType is set, exact build-type matches win, then legacy untyped artifacts.
func SelectArtifact(manifest *Manifest, abiTags []string, buildType string) (*Artifact, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}

	find := func(abiTag, wantedBuildType string) *Artifact {
		for i := range manifest.Artifacts {
			artifact := &manifest.Artifacts[i]
			if artifact.ABITag == abiTag && artifact.BuildType == wantedBuildType {
				return artifact
			}
		}
		return nil
	}

	for _, abiTag := range abiTags {
		if buildType != "" {
			if artifact := find(abiTag, buildType); artifact != nil {
				return artifact, nil
			}
		}
		if artifact := find(abiTag, ""); artifact != nil {
			return artifact, nil
		}
	}

	return nil, fmt.Errorf("no artifact matches abi=%v build_type=%q", abiTags, buildType)
}

// FindArtifactByHashID resolves a manifest artifact by exact hash_id or unique hash_id prefix.
func FindArtifactByHashID(manifest *Manifest, hashID string) (*Artifact, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	hashID = strings.TrimSpace(hashID)
	if hashID == "" {
		return nil, fmt.Errorf("hash_id must not be empty")
	}

	for i := range manifest.Artifacts {
		artifact := &manifest.Artifacts[i]
		if artifact.HashID == hashID {
			return artifact, nil
		}
	}

	var (
		match      *Artifact
		candidates []string
	)
	for i := range manifest.Artifacts {
		artifact := &manifest.Artifacts[i]
		if artifact.HashID == "" || !strings.HasPrefix(artifact.HashID, hashID) {
			continue
		}
		candidates = append(candidates, artifact.HashID)
		if match != nil {
			return nil, fmt.Errorf("hash_id prefix %q is ambiguous in manifest %s@%s: %s", hashID, manifest.Name, manifest.Version, strings.Join(candidates, ", "))
		}
		match = artifact
	}
	if match == nil {
		return nil, fmt.Errorf("artifact with hash_id prefix %q not found in manifest %s@%s", hashID, manifest.Name, manifest.Version)
	}
	return match, nil
}

// S3Client wraps S3 operations for package registry
type S3Client struct {
	client *s3.Client
	bucket string
	prefix string // e.g. "cstow-registry/"
}

// NewS3Client creates an S3 client from registry config.
// Supports three auth methods (priority order):
// 1. CSTOW_REGISTRY_KEY/SECRET env vars
// 2. registry access_key/secret_key
// 3. AWS_PROFILE env var or reg.Profile field (reads ~/.aws/config and ~/.aws/credentials)
// 4. Default AWS credential chain
func NewS3Client(ctx context.Context, reg config.Registry) (*S3Client, error) {
	bucket, prefix := parseBucketURL(reg.URL)
	runtimeCfg, err := resolveRegistryRuntimeConfig(ctx, reg)
	if err != nil {
		return nil, err
	}

	var opts []func(*awsconfig.LoadOptions) error

	// Auth: explicit env vars take priority
	if runtimeCfg.AccessKey != "" && runtimeCfg.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(runtimeCfg.AccessKey, runtimeCfg.SecretKey, "")),
		)
	}

	// Profile support (e.g. "cstow" profile in ~/.aws/config)
	if runtimeCfg.Profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(runtimeCfg.Profile))
	}

	if reg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(reg.Region))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	if cfg.Region == "" && runtimeCfg.EndpointURL != "" {
		cfg.Region = defaultRegionForEndpoint(reg.Provider, runtimeCfg.EndpointURL)
	}
	if runtimeCfg.EndpointURL != "" {
		cfg.BaseEndpoint = aws.String(runtimeCfg.EndpointURL)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if runtimeCfg.UsePathStyle {
			o.UsePathStyle = true
		}
	})

	return &S3Client{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// Upload uploads a package artifact to the registry
func artifactObjectKey(prefix, pkg, version, abiTag, buildType, hashID string) string {
	if hashID != "" {
		return path.Join(prefix, pkg, version, abiTag, buildType, hashID+".tar.zst")
	}
	if buildType != "" {
		return path.Join(prefix, pkg, version, abiTag, buildType+".tar.zst")
	}
	return legacyArtifactObjectKey(prefix, pkg, version, abiTag)
}

func legacyArtifactObjectKey(prefix, pkg, version, abiTag string) string {
	return path.Join(prefix, pkg, version, abiTag+".tar.zst")
}

func (c *S3Client) Upload(ctx context.Context, pkg, version, abiTag, buildType, hashID string, data []byte) error {
	keys := artifactUploadKeys(c.prefix, pkg, version, abiTag, buildType, hashID)
	for _, key := range keys {
		_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
			Body:   bytes.NewReader(data),
		})
		if err != nil {
			return fmt.Errorf("upload %s: %w", key, err)
		}
	}
	return nil
}

func artifactUploadKeys(prefix, pkg, version, abiTag, buildType, hashID string) []string {
	keys := []string{artifactObjectKey(prefix, pkg, version, abiTag, buildType, hashID)}
	if hashID == "" {
		return keys
	}
	if buildType != "" {
		keys = append(keys, artifactObjectKey(prefix, pkg, version, abiTag, buildType, ""))
		return dedupeKeys(keys)
	}
	keys = append(keys, legacyArtifactObjectKey(prefix, pkg, version, abiTag))
	return dedupeKeys(keys)
}

func dedupeKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func artifactDownloadKeys(prefix, pkg, version, abiTag, buildType, hashID string) []string {
	keys := make([]string, 0, 3)
	if hashID != "" {
		keys = append(keys, artifactObjectKey(prefix, pkg, version, abiTag, buildType, hashID))
	}
	if buildType != "" {
		keys = append(keys, artifactObjectKey(prefix, pkg, version, abiTag, buildType, ""))
	}
	keys = append(keys, legacyArtifactObjectKey(prefix, pkg, version, abiTag))
	return dedupeKeys(keys)
}

func hashMatches(data []byte, hashID string) bool {
	if hashID == "" {
		return true
	}
	sum := sha256.Sum256(data)
	actual := fmt.Sprintf("%x", sum)
	return strings.EqualFold(actual, hashID)
}

func downloadFirstMatching(keys []string, hashID string, getter func(key string) ([]byte, error)) ([]byte, error) {
	var lastErr error
	for _, key := range keys {
		data, err := getter(key)
		if err != nil {
			lastErr = err
			continue
		}
		if !hashMatches(data, hashID) {
			lastErr = fmt.Errorf("hash mismatch for key %s", key)
			continue
		}
		return data, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("artifact not found")
	}
	return nil, lastErr
}

// Download downloads a package artifact from the registry
func (c *S3Client) Download(ctx context.Context, pkg, version, abiTag, buildType, hashID string) ([]byte, error) {
	keys := artifactDownloadKeys(c.prefix, pkg, version, abiTag, buildType, hashID)
	data, err := downloadFirstMatching(keys, hashID, func(key string) ([]byte, error) {
		resp, err := c.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	})
	if err != nil {
		return nil, fmt.Errorf("download %s@%s (%s, %s, %s): %w", pkg, version, abiTag, buildType, hashID, err)
	}
	return data, nil
}

// UploadManifest uploads the manifest.toml for a package version
func (c *S3Client) UploadManifest(ctx context.Context, pkg, version string, manifest *Manifest) error {
	key := path.Join(c.prefix, pkg, version, "manifest.toml")

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(manifest); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(buf.Bytes()),
	})
	if err != nil {
		return fmt.Errorf("upload manifest %s: %w", key, err)
	}
	return nil
}

// GetManifest downloads and parses the manifest for a package version
func (c *S3Client) GetManifest(ctx context.Context, pkg, version string) (*Manifest, error) {
	key := path.Join(c.prefix, pkg, version, "manifest.toml")
	resp, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get manifest %s: %w", key, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// ListVersions returns all available versions for a package
func (c *S3Client) ListVersions(ctx context.Context, pkg string) ([]string, error) {
	prefix := path.Join(c.prefix, pkg) + "/"

	resp, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(c.bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, fmt.Errorf("list versions for %s: %w", pkg, err)
	}

	var versions []string
	for _, cp := range resp.CommonPrefixes {
		v := strings.TrimPrefix(*cp.Prefix, prefix)
		v = strings.TrimSuffix(v, "/")
		versions = append(versions, v)
	}
	return versions, nil
}

// parseBucketURL parses "s3://bucket/path" into bucket and prefix
func parseBucketURL(url string) (bucket, prefix string) {
	url = strings.TrimPrefix(url, "s3://")
	parts := strings.SplitN(url, "/", 2)
	bucket = parts[0]
	if len(parts) == 2 {
		prefix = parts[1]
	}
	return
}
