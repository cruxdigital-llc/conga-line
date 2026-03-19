package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

type SecretEntry struct {
	Name        string
	LastChanged string
}

func ListSecrets(ctx context.Context, client *secretsmanager.Client, prefix string) ([]SecretEntry, error) {
	var entries []SecretEntry
	var nextToken *string

	for {
		out, err := client.ListSecrets(ctx, &secretsmanager.ListSecretsInput{
			Filters: []smtypes.Filter{
				{Key: smtypes.FilterNameStringTypeName, Values: []string{prefix}},
			},
			NextToken: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list secrets: %w", err)
		}

		for _, s := range out.SecretList {
			name := aws.ToString(s.Name)
			shortName := strings.TrimPrefix(name, prefix)
			lastChanged := ""
			if s.LastChangedDate != nil {
				lastChanged = s.LastChangedDate.Format("2006-01-02 15:04")
			}
			entries = append(entries, SecretEntry{
				Name:        shortName,
				LastChanged: lastChanged,
			})
		}

		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return entries, nil
}

func SetSecret(ctx context.Context, client *secretsmanager.Client, name, value string) error {
	_, err := client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(name),
		SecretString: aws.String(value),
	})
	if err != nil {
		// If secret doesn't exist, create it
		if isResourceNotFound(err) {
			_, err = client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
				Name:         aws.String(name),
				SecretString: aws.String(value),
			})
			if err != nil {
				return fmt.Errorf("failed to create secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to update secret: %w", err)
	}
	return nil
}

func GetSecretValue(ctx context.Context, client *secretsmanager.Client, name string) (string, error) {
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(name),
	})
	if err != nil {
		if isResourceNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get secret: %w", err)
	}
	return aws.ToString(out.SecretString), nil
}

func DeleteSecret(ctx context.Context, client *secretsmanager.Client, name string) error {
	_, err := client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(name),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	})
	return err
}

func isResourceNotFound(err error) bool {
	var notFound *smtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
