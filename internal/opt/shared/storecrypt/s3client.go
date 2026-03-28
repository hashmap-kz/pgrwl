package storecrypt

import (
	"context"
	"crypto/tls"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type RequestChecksumCalculation int

const (
	// RequestChecksumCalculationUnset is the unset value for RequestChecksumCalculation
	RequestChecksumCalculationUnset RequestChecksumCalculation = iota

	// RequestChecksumCalculationWhenSupported indicates request checksum will be calculated
	// if the operation supports input checksums
	RequestChecksumCalculationWhenSupported

	// RequestChecksumCalculationWhenRequired indicates request checksum will be calculated
	// if required by the operation or if user elects to set a checksum algorithm in request
	RequestChecksumCalculationWhenRequired
)

type S3Config struct {
	EndpointURL                string
	AccessKeyID                string
	SecretAccessKey            string
	Bucket                     string
	Region                     string
	UsePathStyle               bool
	DisableSSL                 bool
	RequestChecksumCalculation RequestChecksumCalculation
}

type S3Client struct {
	client *s3.Client
	bucket string
}

// NewS3Client initializes the S3 client and sets up the bucket name
func NewS3Client(s3Config *S3Config) (*S3Client, error) {
	// https://github.com/aws/aws-sdk-go-v2/issues/1295

	cfg, err := config.LoadDefaultConfig(
		context.Background(),
		config.WithRegion(s3Config.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s3Config.AccessKeyID, s3Config.SecretAccessKey, "")),
		config.WithHTTPClient(&http.Client{
			Transport: &http.Transport{ // <--- here
				TLSClientConfig: &tls.Config{
					//nolint:gosec
					InsecureSkipVerify: s3Config.DisableSSL,
				},
			},
		}),
	)
	if err != nil {
		return nil, err
	}

	cfg.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	cfg.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s3Config.EndpointURL)
		o.UsePathStyle = s3Config.UsePathStyle
		o.RequestChecksumCalculation = aws.RequestChecksumCalculation(s3Config.RequestChecksumCalculation)
	})

	return &S3Client{
		client: client,
		bucket: s3Config.Bucket,
	}, nil
}

func (c *S3Client) Client() *s3.Client {
	return c.client
}

func (c *S3Client) Bucket() string {
	return c.bucket
}
