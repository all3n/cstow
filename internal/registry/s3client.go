package registry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
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
	ABITag    string `toml:"abi_tag"`
	BuildType string `toml:"build_type,omitempty"`
	SHA256    string `toml:"sha256"`
	Size      int64  `toml:"size"`
	URL       string `toml:"url,omitempty"`
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

// S3Client wraps S3 operations for package registry
type S3Client struct {
	client *s3.Client
	bucket string
	prefix string // e.g. "cstow-registry/"
}

// NewS3Client creates an S3 client from registry config.
// Supports three auth methods (priority order):
// 1. CSTOW_REGISTRY_KEY/SECRET env vars
// 2. AWS_PROFILE env var or reg.Profile field (reads ~/.aws/config)
// 3. Default AWS credential chain
func NewS3Client(ctx context.Context, reg config.Registry) (*S3Client, error) {
	bucket, prefix := parseBucketURL(reg.URL)

	var opts []func(*awsconfig.LoadOptions) error

	// Auth: explicit env vars take priority
	key := os.Getenv("CSTOW_REGISTRY_KEY")
	secret := os.Getenv("CSTOW_REGISTRY_SECRET")
	if key != "" && secret != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(key, secret, "")),
		)
	}

	// Profile support (e.g. "cstow" profile in ~/.aws/config)
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		profile = reg.Profile
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}

	// Custom endpoint for non-AWS providers
	if reg.Provider == "cloudflare" || reg.Provider == "minio" || reg.Provider == "custom" {
		endpoint := os.Getenv("CSTOW_REGISTRY_URL")
		if endpoint == "" {
			// Extract just the host from s3://host/path to use as endpoint
			parsed, parseErr := url.Parse(reg.URL)
			if parseErr == nil && parsed.Host != "" {
				endpoint = "https://" + parsed.Host
			} else {
				endpoint = "https://" + strings.TrimPrefix(reg.URL, "s3://")
			}
		}
		customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				URL:               endpoint,
				SigningRegion:     reg.Region,
				HostnameImmutable: true,
			}, nil
		})
		opts = append(opts, awsconfig.WithEndpointResolverWithOptions(customResolver))
	}

	if reg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(reg.Region))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if reg.Provider == "cloudflare" || reg.Provider == "minio" || reg.Provider == "custom" {
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
func artifactObjectKey(prefix, pkg, version, abiTag, buildType string) string {
	return path.Join(prefix, pkg, version, abiTag, buildType+".tar.zst")
}

func legacyArtifactObjectKey(prefix, pkg, version, abiTag string) string {
	return path.Join(prefix, pkg, version, abiTag+".tar.zst")
}

func (c *S3Client) Upload(ctx context.Context, pkg, version, abiTag, buildType string, data []byte) error {
	key := artifactObjectKey(c.prefix, pkg, version, abiTag, buildType)
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
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

// Download downloads a package artifact from the registry
func (c *S3Client) Download(ctx context.Context, pkg, version, abiTag, buildType string) ([]byte, error) {
	keys := []string{legacyArtifactObjectKey(c.prefix, pkg, version, abiTag)}
	if buildType != "" {
		keys = append([]string{artifactObjectKey(c.prefix, pkg, version, abiTag, buildType)}, keys...)
	}
	var lastErr error
	for _, key := range keys {
		resp, err := c.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("artifact not found")
	}
	return nil, fmt.Errorf("download %s@%s (%s, %s): %w", pkg, version, abiTag, buildType, lastErr)
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
