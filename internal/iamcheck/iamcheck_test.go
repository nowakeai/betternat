package iamcheck

import (
	"context"
	"testing"
)

func TestCheckUsesRequiredRuntimeActions(t *testing.T) {
	evaluator := &fakeEvaluator{}
	result, err := Check(context.Background(), evaluator)
	if err != nil {
		t.Fatalf("check iam: %v", err)
	}
	if len(evaluator.actions) != len(RequiredRuntimeActions) {
		t.Fatalf("unexpected actions: %#v", evaluator.actions)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "ec2:ReplaceRoute" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

type fakeEvaluator struct {
	actions []string
}

func (f *fakeEvaluator) Evaluate(_ context.Context, actions []string) (Result, error) {
	f.actions = append([]string(nil), actions...)
	return Result{
		Allowed: actions[:len(actions)-1],
		Missing: []string{"ec2:ReplaceRoute"},
	}, nil
}
