package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// S3Store implements the subset of the S3 API required by the platform. It is
// compatible with AWS S3 and path-style S3 services such as MinIO. Every object
// written by this adapter carries an immutable SHA-256 metadata value, allowing
// exact idempotent replay and integrity verification without trusting an ETag.
type S3Store struct {
	Endpoint     *url.URL
	Bucket       string
	Region       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	PathStyle    bool
	Client       *http.Client
	TempDir      string
	Now          func() time.Time
}

type S3Options struct {
	Endpoint     string
	Bucket       string
	Region       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	PathStyle    bool
	Client       *http.Client
	TempDir      string
}

func NewS3Store(options S3Options) (*S3Store, error) {
	endpoint, err := url.Parse(strings.TrimSpace(options.Endpoint))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("a valid S3 endpoint URL is required")
	}
	if endpoint.Scheme != "https" && endpoint.Scheme != "http" {
		return nil, errors.New("S3 endpoint must use HTTP or HTTPS")
	}
	bucket := strings.TrimSpace(options.Bucket)
	if bucket == "" || strings.ContainsAny(bucket, "/\\\x00") {
		return nil, errors.New("a valid S3 bucket is required")
	}
	region := strings.TrimSpace(options.Region)
	if region == "" {
		region = "us-east-1"
	}
	if strings.TrimSpace(options.AccessKey) == "" || strings.TrimSpace(options.SecretKey) == "" {
		return nil, errors.New("S3 access and secret keys are required")
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/")
	return &S3Store{Endpoint: endpoint, Bucket: bucket, Region: region, AccessKey: strings.TrimSpace(options.AccessKey), SecretKey: options.SecretKey, SessionToken: strings.TrimSpace(options.SessionToken), PathStyle: options.PathStyle, Client: client, TempDir: options.TempDir}, nil
}

func (s *S3Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *S3Store) Put(ctx context.Context, key string, reader io.Reader, maxBytes int64) (Metadata, error) {
	if reader == nil || maxBytes <= 0 {
		return Metadata{}, errors.New("object reader and positive size limit are required")
	}
	if err := validateObjectKey(key); err != nil {
		return Metadata{}, err
	}
	temporary, err := os.CreateTemp(s.TempDir, "campaign-platform-s3-put-*")
	if err != nil {
		return Metadata{}, err
	}
	name := temporary.Name()
	defer os.Remove(name)
	hash := sha256.New()
	size, copyErr := copyContext(ctx, io.MultiWriter(temporary, hash), reader, maxBytes)
	if copyErr != nil {
		temporary.Close()
		return Metadata{}, copyErr
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return Metadata{}, err
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		temporary.Close()
		return Metadata{}, err
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	created := s.now()
	request, err := s.request(ctx, http.MethodPut, key, temporary)
	if err != nil {
		temporary.Close()
		return Metadata{}, err
	}
	request.ContentLength = size
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("If-None-Match", "*")
	request.Header.Set("X-Amz-Meta-Sha256", checksum)
	request.Header.Set("X-Amz-Meta-Size", strconv.FormatInt(size, 10))
	request.Header.Set("X-Amz-Meta-Created-At", created.Format(time.RFC3339Nano))
	if err := s.sign(request, checksum, created); err != nil {
		temporary.Close()
		return Metadata{}, err
	}
	response, err := s.Client.Do(request)
	closeErr := temporary.Close()
	if errors.Is(closeErr, os.ErrClosed) {
		closeErr = nil
	}
	if err != nil {
		return Metadata{}, errors.Join(err, closeErr)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusPreconditionFailed || response.StatusCode == http.StatusConflict {
		existing, statErr := s.Stat(ctx, key)
		if statErr != nil {
			return Metadata{}, statErr
		}
		if existing.Size != size || !strings.EqualFold(existing.SHA256, checksum) {
			return Metadata{}, ErrKeyConflict
		}
		return existing, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Metadata{}, s.responseError(response)
	}
	if closeErr != nil {
		return Metadata{}, closeErr
	}
	return Metadata{Key: key, Size: size, SHA256: checksum, CreatedAt: created}, nil
}

func (s *S3Store) Open(ctx context.Context, key string) (ReadSeekCloser, Metadata, error) {
	if err := validateObjectKey(key); err != nil {
		return nil, Metadata{}, err
	}
	request, err := s.request(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, Metadata{}, err
	}
	now := s.now()
	if err := s.sign(request, emptySHA256, now); err != nil {
		return nil, Metadata{}, err
	}
	response, err := s.Client.Do(request)
	if err != nil {
		return nil, Metadata{}, err
	}
	if response.StatusCode == http.StatusNotFound {
		response.Body.Close()
		return nil, Metadata{}, ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		return nil, Metadata{}, s.responseError(response)
	}
	expectedHash := strings.ToLower(strings.TrimSpace(response.Header.Get("X-Amz-Meta-Sha256")))
	if len(expectedHash) != 64 {
		response.Body.Close()
		return nil, Metadata{}, errors.New("S3 object is missing platform SHA-256 metadata")
	}
	maximum := response.ContentLength
	if maximum < 0 {
		maximum = 2 << 30
	}
	if maximum > 2<<30 {
		response.Body.Close()
		return nil, Metadata{}, ErrTooLarge
	}
	temporary, err := os.CreateTemp(s.TempDir, "campaign-platform-s3-get-*")
	if err != nil {
		response.Body.Close()
		return nil, Metadata{}, err
	}
	name := temporary.Name()
	hash := sha256.New()
	limit := maximum
	if limit == 0 {
		limit = 1
	}
	size, copyErr := copyContext(ctx, io.MultiWriter(temporary, hash), response.Body, limit)
	bodyCloseErr := response.Body.Close()
	if copyErr != nil || bodyCloseErr != nil {
		temporary.Close()
		os.Remove(name)
		return nil, Metadata{}, errors.Join(copyErr, bodyCloseErr)
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	if !hmac.Equal([]byte(checksum), []byte(expectedHash)) {
		temporary.Close()
		os.Remove(name)
		return nil, Metadata{}, errors.New("S3 object checksum does not match approved metadata")
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		temporary.Close()
		os.Remove(name)
		return nil, Metadata{}, err
	}
	created := parseS3Time(response.Header.Get("X-Amz-Meta-Created-At"), response.Header.Get("Last-Modified"), now)
	return &temporaryObject{File: temporary, path: name}, Metadata{Key: key, Size: size, SHA256: checksum, CreatedAt: created}, nil
}

func (s *S3Store) Stat(ctx context.Context, key string) (Metadata, error) {
	if err := validateObjectKey(key); err != nil {
		return Metadata{}, err
	}
	request, err := s.request(ctx, http.MethodHead, key, nil)
	if err != nil {
		return Metadata{}, err
	}
	now := s.now()
	if err := s.sign(request, emptySHA256, now); err != nil {
		return Metadata{}, err
	}
	response, err := s.Client.Do(request)
	if err != nil {
		return Metadata{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return Metadata{}, ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Metadata{}, s.responseError(response)
	}
	checksum := strings.ToLower(strings.TrimSpace(response.Header.Get("X-Amz-Meta-Sha256")))
	if len(checksum) != 64 {
		return Metadata{}, errors.New("S3 object is missing platform SHA-256 metadata")
	}
	size := response.ContentLength
	if value := response.Header.Get("X-Amz-Meta-Size"); value != "" {
		parsed, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || parsed < 0 || (size >= 0 && parsed != size) {
			return Metadata{}, errors.New("S3 object size metadata is invalid")
		}
		size = parsed
	}
	return Metadata{Key: key, Size: size, SHA256: checksum, CreatedAt: parseS3Time(response.Header.Get("X-Amz-Meta-Created-At"), response.Header.Get("Last-Modified"), now)}, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	if err := validateObjectKey(key); err != nil {
		return err
	}
	request, err := s.request(ctx, http.MethodDelete, key, nil)
	if err != nil {
		return err
	}
	if err := s.sign(request, emptySHA256, s.now()); err != nil {
		return err
	}
	response, err := s.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || (response.StatusCode >= 200 && response.StatusCode < 300) {
		return nil
	}
	return s.responseError(response)
}

type temporaryObject struct {
	*os.File
	path string
}

func (t *temporaryObject) Close() error {
	if t == nil || t.File == nil {
		return nil
	}
	err := t.File.Close()
	removeErr := os.Remove(t.path)
	t.File = nil
	return errors.Join(err, removeErr)
}

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func (s *S3Store) request(ctx context.Context, method, key string, body io.Reader) (*http.Request, error) {
	base := *s.Endpoint
	if s.PathStyle {
		base.Path = strings.TrimSuffix(base.Path, "/") + "/" + s.Bucket + "/" + key
	} else {
		base.Host = s.Bucket + "." + base.Host
		base.Path = strings.TrimSuffix(base.Path, "/") + "/" + key
	}
	return http.NewRequestWithContext(ctx, method, base.String(), body)
}

func validateObjectKey(key string) error {
	key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	if key == "" || strings.HasPrefix(key, "/") || strings.ContainsRune(key, '\x00') || path.Clean(key) != key || key == "." || key == ".." || strings.HasPrefix(key, "../") {
		return ErrInvalidKey
	}
	return nil
}

func (s *S3Store) sign(request *http.Request, payloadHash string, at time.Time) error {
	at = at.UTC()
	amzDate := at.Format("20060102T150405Z")
	date := at.Format("20060102")
	request.Header.Set("X-Amz-Date", amzDate)
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if s.SessionToken != "" {
		request.Header.Set("X-Amz-Security-Token", s.SessionToken)
	}
	canonicalHeaders, signedHeaders := canonicalS3Headers(request)
	canonicalRequest := strings.Join([]string{request.Method, request.URL.EscapedPath(), canonicalQuery(request.URL.Query()), canonicalHeaders, signedHeaders, payloadHash}, "\n")
	scope := date + "/" + s.Region + "/s3/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(requestHash[:])
	dateKey := hmacSHA256([]byte("AWS4"+s.SecretKey), date)
	regionKey := hmacSHA256(dateKey, s.Region)
	serviceKey := hmacSHA256(regionKey, "s3")
	signingKey := hmacSHA256(serviceKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.AccessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return nil
}

func canonicalS3Headers(request *http.Request) (string, string) {
	values := map[string]string{"host": request.URL.Host}
	for name, entries := range request.Header {
		lower := strings.ToLower(strings.TrimSpace(name))
		if lower == "authorization" || (lower != "if-none-match" && !strings.HasPrefix(lower, "x-amz-")) {
			continue
		}
		joined := strings.Join(entries, ",")
		values[lower] = strings.Join(strings.Fields(joined), " ")
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for _, key := range keys {
		canonical.WriteString(key)
		canonical.WriteByte(':')
		canonical.WriteString(values[key])
		canonical.WriteByte('\n')
	}
	return canonical.String(), strings.Join(keys, ";")
}

func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0)
	for _, key := range keys {
		entries := append([]string(nil), values[key]...)
		sort.Strings(entries)
		for _, value := range entries {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func parseS3Time(metadata, lastModified string, fallback time.Time) time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, metadata); err == nil {
		return parsed.UTC()
	}
	if parsed, err := http.ParseTime(lastModified); err == nil {
		return parsed.UTC()
	}
	return fallback.UTC()
}

func (s *S3Store) responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	return fmt.Errorf("S3 %s %s returned %d: %s", response.Request.Method, response.Request.URL.Redacted(), response.StatusCode, strings.TrimSpace(string(body)))
}
