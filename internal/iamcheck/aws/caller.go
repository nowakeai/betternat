package awsiamcheck

import (
	"context"
	"fmt"
	"regexp"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type STSAPI interface {
	GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

var assumedRoleARNPattern = regexp.MustCompile(`^arn:([^:]+):sts::([0-9]{12}):assumed-role/([^/]+)/[^/]+$`)

func ResolveCurrentRoleARN(ctx context.Context, region string) (string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return "", fmt.Errorf("load aws config: %w", err)
	}
	return ResolveCurrentRoleARNFromClient(ctx, sts.NewFromConfig(cfg))
}

func ResolveCurrentRoleARNFromClient(ctx context.Context, client STSAPI) (string, error) {
	if client == nil {
		return "", fmt.Errorf("sts client is required")
	}
	output, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("aws sts GetCallerIdentity: %w", err)
	}
	arn := awssdk.ToString(output.Arn)
	if arn == "" {
		return "", fmt.Errorf("caller identity returned empty arn")
	}
	if match := assumedRoleARNPattern.FindStringSubmatch(arn); len(match) == 4 {
		return fmt.Sprintf("arn:%s:iam::%s:role/%s", match[1], match[2], match[3]), nil
	}
	return arn, nil
}
