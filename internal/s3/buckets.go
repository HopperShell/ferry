// internal/s3/buckets.go
package s3util

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// BucketEntry represents an S3 bucket for the picker.
type BucketEntry struct {
	Name    string
	Region  string
	Profile string // AWS profile name ("default" if none)
}

// ListAllBuckets discovers AWS profiles from ~/.aws/config and lists buckets
// for each profile that has valid credentials. Returns nil if no profiles found.
func ListAllBuckets(ctx context.Context) []BucketEntry {
	profiles := discoverProfiles()
	if len(profiles) == 0 {
		// No config file; try default credentials.
		buckets, _ := listBucketsForProfile(ctx, "")
		return buckets
	}

	var all []BucketEntry
	for _, profile := range profiles {
		buckets, _ := listBucketsForProfile(ctx, profile)
		all = append(all, buckets...)
	}
	return all
}

// listBucketsForProfile lists buckets accessible with the given profile.
// Pass "" for the default profile.
func listBucketsForProfile(ctx context.Context, profile string) ([]BucketEntry, error) {
	var opts []func(*config.LoadOptions) error
	if profile != "" && profile != "default" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}

	// Check credentials are valid.
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil || !creds.HasKeys() {
		return nil, nil
	}

	client := s3.NewFromConfig(cfg)
	result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, nil
	}

	displayProfile := profile
	if displayProfile == "" {
		displayProfile = "default"
	}

	entries := make([]BucketEntry, 0, len(result.Buckets))
	for _, b := range result.Buckets {
		name := ""
		if b.Name != nil {
			name = *b.Name
		}
		entries = append(entries, BucketEntry{
			Name:    name,
			Profile: displayProfile,
		})
	}
	return entries, nil
}

// discoverProfiles parses ~/.aws/config to find all profile names.
// Returns profile names (e.g., ["default", "prod", "staging"]).
func discoverProfiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	f, err := os.Open(filepath.Join(home, ".aws", "config"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var profiles []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[profile ") && strings.HasSuffix(line, "]") {
			name := strings.TrimPrefix(line, "[profile ")
			name = strings.TrimSuffix(name, "]")
			profiles = append(profiles, name)
		} else if line == "[default]" {
			profiles = append(profiles, "default")
		}
	}
	return profiles
}
