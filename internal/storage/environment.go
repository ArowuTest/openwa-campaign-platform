package storage

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// NewObjectStoreFromEnvironment selects the portable object-store adapter used
// by every service. Filesystem storage remains suitable for development and a
// dedicated encrypted VPS volume; S3 mode supports MinIO and AWS-compatible
// services without changing domain code.
func NewObjectStoreFromEnvironment(defaultRoot string) (ObjectStore, error) {
	driver := strings.ToLower(strings.TrimSpace(os.Getenv("OBJECT_STORE_DRIVER")))
	if driver == "" {
		driver = "filesystem"
	}
	switch driver {
	case "filesystem":
		root := strings.TrimSpace(os.Getenv("OBJECT_STORE_ROOT"))
		if root == "" {
			root = strings.TrimSpace(defaultRoot)
		}
		return NewFileSystemStore(root)
	case "s3", "minio":
		pathStyle := true
		if raw := strings.TrimSpace(os.Getenv("S3_PATH_STYLE")); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, errors.New("S3_PATH_STYLE must be true or false")
			}
			pathStyle = parsed
		}
		directUploadAllowed := false
		if raw := strings.TrimSpace(os.Getenv("S3_DIRECT_UPLOAD_ENABLED")); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, errors.New("S3_DIRECT_UPLOAD_ENABLED must be true or false")
			}
			directUploadAllowed = parsed
		}
		endpoint := strings.TrimSpace(os.Getenv("S3_ENDPOINT"))
		allowInsecure, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("S3_ALLOW_INSECURE")))
		if strings.HasPrefix(strings.ToLower(endpoint), "http://") && !allowInsecure {
			return nil, errors.New("HTTP S3 endpoints require S3_ALLOW_INSECURE=true")
		}
		return NewS3Store(S3Options{
			Endpoint: endpoint, DirectUploadEndpoint: strings.TrimSpace(os.Getenv("S3_DIRECT_UPLOAD_ENDPOINT")),
			Bucket: os.Getenv("S3_BUCKET"), Region: os.Getenv("S3_REGION"),
			AccessKey: os.Getenv("S3_ACCESS_KEY_ID"), SecretKey: os.Getenv("S3_SECRET_ACCESS_KEY"), SessionToken: os.Getenv("S3_SESSION_TOKEN"),
			PathStyle: pathStyle, DirectUploadAllowed: directUploadAllowed,
			TempDir: os.Getenv("OBJECT_STORE_TEMP_DIR"), Client: &http.Client{Timeout: 5 * time.Minute},
		})
	default:
		return nil, errors.New("OBJECT_STORE_DRIVER must be filesystem or s3")
	}
}
