package awsiamcheck

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func TestResolveCurrentRoleARNConvertsAssumedRole(t *testing.T) {
	roleARN, err := ResolveCurrentRoleARNFromClient(context.Background(), fakeSTS{
		arn: "arn:aws:sts::123456789012:assumed-role/betternat-prod-agent/i-abc",
	})
	if err != nil {
		t.Fatalf("resolve current role arn: %v", err)
	}
	if roleARN != "arn:aws:iam::123456789012:role/betternat-prod-agent" {
		t.Fatalf("unexpected role arn: %s", roleARN)
	}
}

func TestResolveCurrentRoleARNPreservesPartition(t *testing.T) {
	roleARN, err := ResolveCurrentRoleARNFromClient(context.Background(), fakeSTS{
		arn: "arn:aws-us-gov:sts::123456789012:assumed-role/betternat-prod-agent/i-abc",
	})
	if err != nil {
		t.Fatalf("resolve current role arn: %v", err)
	}
	if roleARN != "arn:aws-us-gov:iam::123456789012:role/betternat-prod-agent" {
		t.Fatalf("unexpected role arn: %s", roleARN)
	}
}

type fakeSTS struct {
	arn string
}

func (f fakeSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return &sts.GetCallerIdentityOutput{Arn: awssdk.String(f.arn)}, nil
}
