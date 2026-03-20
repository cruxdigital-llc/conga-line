package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Compile-time assertions that concrete SDK clients satisfy our interfaces.
var (
	_ SSMClient            = (*ssm.Client)(nil)
	_ SecretsManagerClient = (*secretsmanager.Client)(nil)
	_ EC2Client            = (*ec2.Client)(nil)
	_ STSClient            = (*sts.Client)(nil)
)

type Clients struct {
	Config         aws.Config
	EC2            EC2Client
	SSM            SSMClient
	STS            STSClient
	SecretsManager SecretsManagerClient
}

func NewClients(ctx context.Context, region, profile string) (*Clients, error) {
	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(region))

	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &Clients{
		Config:         cfg,
		EC2:            ec2.NewFromConfig(cfg),
		SSM:            ssm.NewFromConfig(cfg),
		STS:            sts.NewFromConfig(cfg),
		SecretsManager: secretsmanager.NewFromConfig(cfg),
	}, nil
}
