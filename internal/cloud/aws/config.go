package awscloud

import (
	"context"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

const (
	defaultMaxAttempts = 4
	defaultMaxBackoff  = 3 * time.Second
)

func LoadConfig(ctx context.Context, region string, optFns ...func(*awsconfig.LoadOptions) error) (awssdk.Config, error) {
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		awsconfig.WithRetryer(func() awssdk.Retryer {
			return newRetryer()
		}),
	}
	options = append(options, optFns...)
	return awsconfig.LoadDefaultConfig(ctx, options...)
}

func newRetryer() awssdk.Retryer {
	return retry.NewStandard(func(o *retry.StandardOptions) {
		o.MaxAttempts = defaultMaxAttempts
		o.MaxBackoff = defaultMaxBackoff
	})
}
