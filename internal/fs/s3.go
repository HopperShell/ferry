package fs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const mtimeMetadataKey = "ferry-mtime"

type S3FS struct {
	client   *s3.Client
	uploader *manager.Uploader
	bucket   string
	prefix   string
}

func NewS3FS(client *s3.Client, bucket, prefix string) *S3FS {
	return &S3FS{
		client:   client,
		uploader: manager.NewUploader(client),
		bucket:   bucket,
		prefix:   prefix,
	}
}

func (s *S3FS) List(p string) ([]Entry, error) {
	fullPrefix := s.toKey(p)
	if fullPrefix != "" && !strings.HasSuffix(fullPrefix, "/") {
		fullPrefix += "/"
	}

	var entries []Entry
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket:    aws.String(s.bucket),
		Prefix:    aws.String(fullPrefix),
		Delimiter: aws.String("/"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			return nil, fmt.Errorf("s3 list: %w", err)
		}

		for _, cp := range page.CommonPrefixes {
			dirName := path.Base(strings.TrimSuffix(*cp.Prefix, "/"))
			entries = append(entries, Entry{
				Name:  dirName,
				Path:  s.fromKey(strings.TrimSuffix(*cp.Prefix, "/")),
				IsDir: true,
				Mode:  os.ModeDir | 0o755,
			})
		}

		for _, obj := range page.Contents {
			key := *obj.Key
			if key == fullPrefix {
				continue
			}
			if strings.HasSuffix(key, "/") && obj.Size != nil && *obj.Size == 0 {
				continue
			}

			name := path.Base(key)
			mtime := s.objectMtime(obj.LastModified, nil)

			var size int64
			if obj.Size != nil {
				size = *obj.Size
			}

			entries = append(entries, Entry{
				Name:    name,
				Path:    s.fromKey(key),
				Size:    size,
				ModTime: mtime,
				Mode:    0o644,
				IsDir:   false,
			})
		}
	}

	return entries, nil
}

func (s *S3FS) Stat(p string) (Entry, error) {
	key := s.toKey(p)

	head, err := s.client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		mtime := s.objectMtime(head.LastModified, head.Metadata)
		var size int64
		if head.ContentLength != nil {
			size = *head.ContentLength
		}
		return Entry{
			Name:    path.Base(key),
			Path:    p,
			Size:    size,
			ModTime: mtime,
			Mode:    0o644,
			IsDir:   false,
		}, nil
	}

	listPrefix := key
	if !strings.HasSuffix(listPrefix, "/") {
		listPrefix += "/"
	}
	result, err := s.client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		Prefix:  aws.String(listPrefix),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return Entry{}, fmt.Errorf("s3 stat: %w", err)
	}
	if result.KeyCount != nil && *result.KeyCount > 0 {
		return Entry{
			Name:  path.Base(key),
			Path:  p,
			IsDir: true,
			Mode:  os.ModeDir | 0o755,
		}, nil
	}

	return Entry{}, fmt.Errorf("s3 stat: %s not found", p)
}

func (s *S3FS) Read(p string, w io.Writer) error {
	key := s.toKey(p)
	result, err := s.client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 read: %w", err)
	}
	defer result.Body.Close()
	_, err = io.Copy(w, result.Body)
	return err
}

func (s *S3FS) Write(p string, r io.Reader, perm os.FileMode) error {
	key := s.toKey(p)
	_, err := s.uploader.Upload(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   r,
	})
	if err != nil {
		return fmt.Errorf("s3 write: %w", err)
	}
	return nil
}

func (s *S3FS) Mkdir(p string, perm os.FileMode) error {
	key := s.toKey(p)
	if !strings.HasSuffix(key, "/") {
		key += "/"
	}
	_, err := s.client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(nil),
		ContentLength: aws.Int64(0),
	})
	if err != nil {
		return fmt.Errorf("s3 mkdir: %w", err)
	}
	return nil
}

func (s *S3FS) Remove(p string) error {
	key := s.toKey(p)
	_, err := s.client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	prefix := key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, pageErr := paginator.NextPage(context.TODO())
		if pageErr != nil {
			break
		}
		if len(page.Contents) == 0 {
			break
		}
		objects := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			objects = append(objects, types.ObjectIdentifier{Key: obj.Key})
		}
		_, err = s.client.DeleteObjects(context.TODO(), &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("s3 remove batch: %w", err)
		}
	}
	return nil
}

func (s *S3FS) Rename(oldPath, newPath string) error {
	oldKey := s.toKey(oldPath)
	newKey := s.toKey(newPath)
	_, err := s.client.CopyObject(context.TODO(), &s3.CopyObjectInput{
		Bucket:     aws.String(s.bucket),
		CopySource: aws.String(s.bucket + "/" + oldKey),
		Key:        aws.String(newKey),
	})
	if err != nil {
		return fmt.Errorf("s3 rename (copy): %w", err)
	}
	_, err = s.client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(oldKey),
	})
	if err != nil {
		return fmt.Errorf("s3 rename (delete old): %w", err)
	}
	return nil
}

func (s *S3FS) Chmod(path string, perm os.FileMode) error {
	return nil
}

func (s *S3FS) Chtimes(p string, mtime time.Time) error {
	key := s.toKey(p)
	_, err := s.client.CopyObject(context.TODO(), &s3.CopyObjectInput{
		Bucket:            aws.String(s.bucket),
		CopySource:        aws.String(s.bucket + "/" + key),
		Key:               aws.String(key),
		MetadataDirective: types.MetadataDirectiveReplace,
		Metadata: map[string]string{
			mtimeMetadataKey: strconv.FormatInt(mtime.Unix(), 10),
		},
	})
	if err != nil {
		return fmt.Errorf("s3 chtimes: %w", err)
	}
	return nil
}

func (s *S3FS) HomeDir() (string, error) {
	return "/", nil
}

func (s *S3FS) toKey(p string) string {
	p = strings.TrimPrefix(p, "/")
	if s.prefix == "" {
		return p
	}
	if p == "" {
		return strings.TrimSuffix(s.prefix, "/")
	}
	return s.prefix + p
}

func (s *S3FS) fromKey(key string) string {
	key = strings.TrimPrefix(key, s.prefix)
	if key == "" {
		return "/"
	}
	return "/" + key
}

func (s *S3FS) objectMtime(lastModified *time.Time, metadata map[string]string) time.Time {
	if metadata != nil {
		if raw, ok := metadata[mtimeMetadataKey]; ok {
			if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return time.Unix(unix, 0)
			}
		}
	}
	if lastModified != nil {
		return *lastModified
	}
	return time.Time{}
}
