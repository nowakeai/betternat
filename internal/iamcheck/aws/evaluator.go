package awsiamcheck

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/nowakeai/betternat/internal/iamcheck"
)

type IAMAPI interface {
	SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
}

type Evaluator struct {
	client    IAMAPI
	policyARN string
}

func New(ctx context.Context, region string, policySourceARN string) (*Evaluator, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return NewFromClient(iam.NewFromConfig(cfg), policySourceARN), nil
}

func NewFromClient(client IAMAPI, policySourceARN string) *Evaluator {
	return &Evaluator{client: client, policyARN: policySourceARN}
}

func (e *Evaluator) Evaluate(ctx context.Context, actions []string) (iamcheck.Result, error) {
	if e.client == nil {
		return iamcheck.Result{}, fmt.Errorf("iam client is required")
	}
	if e.policyARN == "" {
		return iamcheck.Result{}, fmt.Errorf("policy source arn is required")
	}
	output, err := e.client.SimulatePrincipalPolicy(ctx, &iam.SimulatePrincipalPolicyInput{
		PolicySourceArn: awssdk.String(e.policyARN),
		ActionNames:     actions,
		ResourceArns:    []string{"*"},
	})
	if err != nil {
		return iamcheck.Result{}, fmt.Errorf("simulate principal policy: %w", err)
	}
	result := iamcheck.Result{}
	for _, evaluation := range output.EvaluationResults {
		action := awssdk.ToString(evaluation.EvalActionName)
		if evaluation.EvalDecision == types.PolicyEvaluationDecisionTypeAllowed {
			result.Allowed = append(result.Allowed, action)
		} else {
			result.Missing = append(result.Missing, action)
		}
	}
	return result, nil
}
