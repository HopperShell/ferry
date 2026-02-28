// internal/s3/client.go
package s3util

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type ConnectResult struct {
	Client *s3.Client
	Bucket string
	Prefix string
	Region string
}

func ParseS3URI(uri string) (bucket, prefix string, err error) {
	uri = strings.TrimPrefix(uri, "s3://")
	if uri == "" {
		return "", "", fmt.Errorf("empty S3 URI")
	}
	parts := strings.SplitN(uri, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		prefix = parts[1]
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}
	return bucket, prefix, nil
}

func Connect(ctx context.Context, bucket, region, profile string) (*ConnectResult, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	if profile != "" && profile != "default" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	// Build S3 client options.
	var s3Opts []func(*s3.Options)

	// Support custom endpoints (MinIO, R2, B2, LocalStack, etc.)
	// via AWS_ENDPOINT_URL env var or endpoint_url in profile config.
	if endpoint := os.Getenv("AWS_ENDPOINT_URL"); endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(cfg, s3Opts...)

	if region == "" {
		region = cfg.Region
		if region == "" {
			region = "us-east-1"
		}
		loc, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
			Bucket: aws.String(bucket),
		})
		if err == nil && loc.LocationConstraint != "" {
			region = string(loc.LocationConstraint)
			cfg.Region = region
			client = s3.NewFromConfig(cfg, s3Opts...)
		}
	}

	return &ConnectResult{
		Client: client,
		Bucket: bucket,
		Region: region,
	}, nil
}

func HasCredentials(ctx context.Context) bool {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return false
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return false
	}
	return creds.HasKeys()
}
