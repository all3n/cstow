package registry

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
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

// S3Client wraps S3 operations for package registry
type S3Client struct {
	client *s3.Client
	bucket string
	prefix string // e.g. "cstow-registry/"
}

type registryRuntimeConfig struct {
	Profile      string
	EndpointURL  string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
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

func resolveRegistryRuntimeConfig(ctx context.Context, reg config.Registry) (registryRuntimeConfig, error) {
	cfg := registryRuntimeConfig{
		Profile:      reg.Profile,
		EndpointURL:  reg.EndpointURL,
		UsePathStyle: needsPathStyle(reg.Provider, reg.EndpointURL),
	}

	if envProfile := os.Getenv("AWS_PROFILE"); envProfile != "" {
		cfg.Profile = envProfile
	}

	if envEndpoint := os.Getenv("CSTOW_REGISTRY_URL"); envEndpoint != "" {
		cfg.EndpointURL = envEndpoint
		cfg.UsePathStyle = true
	} else if cfg.EndpointURL == "" && cfg.Profile != "" {
		endpoint, err := loadAWSProfileS3Endpoint(ctx, cfg.Profile)
		if err != nil {
			return registryRuntimeConfig{}, err
		}
		if endpoint != "" {
			cfg.EndpointURL = endpoint
			cfg.UsePathStyle = true
		}
	}

	if envKey := os.Getenv("CSTOW_REGISTRY_KEY"); envKey != "" {
		if envSecret := os.Getenv("CSTOW_REGISTRY_SECRET"); envSecret != "" {
			cfg.AccessKey = envKey
			cfg.SecretKey = envSecret
			return cfg, nil
		}
	}

	if reg.AccessKey != "" && reg.SecretKey != "" {
		cfg.AccessKey = reg.AccessKey
		cfg.SecretKey = reg.SecretKey
	}

	return cfg, nil
}

func loadAWSProfileS3Endpoint(ctx context.Context, profile string) (string, error) {
	sharedCfg, err := awsconfig.LoadSharedConfigProfile(ctx, profile)
	if err == nil {
		endpoint, found, endpointErr := sharedCfg.GetServiceBaseEndpoint(ctx, "S3")
		if endpointErr != nil {
			return "", fmt.Errorf("resolve shared S3 endpoint for profile %s: %w", profile, endpointErr)
		}
		if found && endpoint != "" {
			return endpoint, nil
		}
	}

	configPath := os.Getenv("AWS_CONFIG_FILE")
	if configPath == "" {
		configPath = awsconfig.DefaultSharedConfigFilename()
	}
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", nil
		}
		return "", fmt.Errorf("read AWS config %s: %w", configPath, readErr)
	}

	return parseNestedS3Endpoint(string(data), profile), nil
}

func parseNestedS3Endpoint(content, profile string) string {
	targetSections := []string{profile}
	if profile == "default" {
		targetSections = append(targetSections, "default")
	} else {
		targetSections = append(targetSections, "profile "+profile)
	}

	currentSection := ""
	inS3Block := false
	s3Indent := -1

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			currentSection = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			inS3Block = false
			s3Indent = -1
			continue
		}

		if !matchesAWSProfileSection(currentSection, targetSections) {
			continue
		}

		indent := leadingWhitespace(line)
		if inS3Block {
			if indent <= s3Indent {
				inS3Block = false
			} else if key, value, ok := parseKeyValue(trimmed); ok && strings.EqualFold(key, "endpoint_url") {
				return value
			}
		}

		key, value, ok := parseKeyValue(trimmed)
		if !ok || !strings.EqualFold(key, "s3") {
			continue
		}
		if nestedKey, nestedValue, nestedOK := parseKeyValue(value); nestedOK && strings.EqualFold(nestedKey, "endpoint_url") {
			return nestedValue
		}
		inS3Block = true
		s3Indent = indent
	}

	return ""
}

func matchesAWSProfileSection(section string, targets []string) bool {
	for _, target := range targets {
		if strings.EqualFold(section, target) {
			return true
		}
	}
	return false
}

func parseKeyValue(line string) (string, string, bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func leadingWhitespace(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func needsPathStyle(provider, endpoint string) bool {
	if endpoint != "" {
		return true
	}
	switch strings.ToLower(provider) {
	case "cloudflare", "minio", "custom":
		return true
	default:
		return false
	}
}

func defaultRegionForEndpoint(provider, endpoint string) string {
	if strings.EqualFold(provider, "cloudflare") || strings.Contains(strings.ToLower(endpoint), ".r2.cloudflarestorage.com") {
		return "auto"
	}
	return "us-east-1"
}

// Upload uploads a package artifact to the registry
func artifactObjectKey(prefix, pkg, version, abiTag, buildType, hashID string) string {
	if hashID == "" {
		return legacyArtifactObjectKey(prefix, pkg, version, abiTag)
	}
	return path.Join(prefix, pkg, version, abiTag, buildType, hashID+".tar.zst")
}

func legacyArtifactObjectKey(prefix, pkg, version, abiTag string) string {
	return path.Join(prefix, pkg, version, abiTag+".tar.zst")
}

func (c *S3Client) Upload(ctx context.Context, pkg, version, abiTag, buildType string, data []byte) error {
	key := artifactObjectKey(c.prefix, pkg, version, abiTag, buildType, "")
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
		keys = append([]string{artifactObjectKey(c.prefix, pkg, version, abiTag, buildType, "")}, keys...)
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
