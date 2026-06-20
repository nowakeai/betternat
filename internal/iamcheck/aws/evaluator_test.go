package awsiamcheck

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
)

func TestEvaluate(t *testing.T) {
	client := &fakeIAM{
		output: &iam.SimulatePrincipalPolicyOutput{
			EvaluationResults: []types.EvaluationResult{
				{
					EvalActionName: awssdk.String("ec2:ReplaceRoute"),
					EvalDecision:   types.PolicyEvaluationDecisionTypeAllowed,
				},
				{
					EvalActionName: awssdk.String("dynamodb:UpdateItem"),
					EvalDecision:   types.PolicyEvaluationDecisionTypeExplicitDeny,
				},
			},
		},
	}
	evaluator := NewFromClient(client, "arn:aws:iam::123:role/betternat-agent")

	result, err := evaluator.Evaluate(context.Background(), []string{"ec2:ReplaceRoute", "dynamodb:UpdateItem"})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if awssdk.ToString(client.input.PolicySourceArn) != "arn:aws:iam::123:role/betternat-agent" {
		t.Fatalf("unexpected policy source: %#v", client.input)
	}
	if len(client.input.ActionNames) != 2 {
		t.Fatalf("unexpected actions: %#v", client.input.ActionNames)
	}
	if len(result.Allowed) != 1 || result.Allowed[0] != "ec2:ReplaceRoute" {
		t.Fatalf("unexpected allowed actions: %#v", result)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "dynamodb:UpdateItem" {
		t.Fatalf("unexpected missing actions: %#v", result)
	}
}

type fakeIAM struct {
	input  *iam.SimulatePrincipalPolicyInput
	output *iam.SimulatePrincipalPolicyOutput
}

func (f *fakeIAM) SimulatePrincipalPolicy(_ context.Context, params *iam.SimulatePrincipalPolicyInput, _ ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
	f.input = params
	return f.output, nil
}
