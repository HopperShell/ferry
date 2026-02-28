// internal/s3/buckets.go
package s3util

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type BucketEntry struct {
	Name   string
	Region string
}

func ListBuckets(ctx context.Context) ([]BucketEntry, error) {
	if !HasCredentials(ctx) {
		return nil, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, nil
	}

	client := s3.NewFromConfig(cfg)
	result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, nil
	}

	entries := make([]BucketEntry, 0, len(result.Buckets))
	for _, b := range result.Buckets {
		name := ""
		if b.Name != nil {
			name = *b.Name
		}
		entries = append(entries, BucketEntry{Name: name})
	}
	return entries, nil
}
