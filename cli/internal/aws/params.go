package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

func GetParameter(ctx context.Context, client *ssm.Client, name string) (string, error) {
	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name: aws.String(name),
	})
	if err != nil {
		return "", fmt.Errorf("parameter %s not found: %w", name, err)
	}
	return aws.ToString(out.Parameter.Value), nil
}

func PutParameter(ctx context.Context, client *ssm.Client, name, value string) error {
	_, err := client.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(name),
		Value:     aws.String(value),
		Type:      ssmtypes.ParameterTypeString,
		Overwrite: aws.Bool(true),
	})
	return err
}

func DeleteParameter(ctx context.Context, client *ssm.Client, name string) error {
	_, err := client.DeleteParameter(ctx, &ssm.DeleteParameterInput{
		Name: aws.String(name),
	})
	return err
}

type ParameterEntry struct {
	Name  string
	Value string
}

func GetParametersByPath(ctx context.Context, client *ssm.Client, path string) ([]ParameterEntry, error) {
	var entries []ParameterEntry
	var nextToken *string

	for {
		out, err := client.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
			Path:      aws.String(path),
			NextToken: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list parameters under %s: %w", path, err)
		}

		for _, p := range out.Parameters {
			name := aws.ToString(p.Name)
			// Skip by-iam/ sub-path entries
			if strings.Contains(name, "/by-iam/") {
				continue
			}
			entries = append(entries, ParameterEntry{
				Name:  name,
				Value: aws.ToString(p.Value),
			})
		}

		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return entries, nil
}
