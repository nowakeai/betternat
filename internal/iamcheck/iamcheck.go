package iamcheck

import (
	"context"
	"fmt"
)

var RequiredRuntimeActions = []string{
	"autoscaling:DescribeAutoScalingGroups",
	"ec2:AssociateAddress",
	"ec2:DescribeAddresses",
	"ec2:DescribeInstanceAttribute",
	"ec2:DescribeRouteTables",
	"ec2:ReplaceRoute",
	"dynamodb:DeleteItem",
	"dynamodb:GetItem",
	"dynamodb:UpdateItem",
	"iam:SimulatePrincipalPolicy",
	"sts:GetCallerIdentity",
}

type Result struct {
	Allowed []string
	Missing []string
}

type Evaluator interface {
	Evaluate(ctx context.Context, actions []string) (Result, error)
}

func Check(ctx context.Context, evaluator Evaluator) (Result, error) {
	if evaluator == nil {
		return Result{}, fmt.Errorf("iam evaluator is required")
	}
	return evaluator.Evaluate(ctx, append([]string(nil), RequiredRuntimeActions...))
}
