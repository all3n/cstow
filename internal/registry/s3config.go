package registry

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	"github.com/all3n/cstow/internal/config"
)

type registryRuntimeConfig struct {
	Profile      string
	EndpointURL  string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
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
