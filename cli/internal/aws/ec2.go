package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func FindInstanceByTag(ctx context.Context, client *ec2.Client, tagName string) (string, error) {
	out, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{Name: aws.String("tag:Name"), Values: []string{tagName}},
			{Name: aws.String("instance-state-name"), Values: []string{"running"}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to describe instances: %w", err)
	}

	var instanceID string
	for _, r := range out.Reservations {
		for _, i := range r.Instances {
			if instanceID != "" {
				return "", fmt.Errorf("multiple running instances found with tag Name=%s", tagName)
			}
			instanceID = aws.ToString(i.InstanceId)
		}
	}

	if instanceID == "" {
		return "", fmt.Errorf("no running instance found with tag Name=%s", tagName)
	}

	return instanceID, nil
}

func StopInstance(ctx context.Context, client *ec2.Client, instanceID string) error {
	_, err := client.StopInstances(ctx, &ec2.StopInstancesInput{
		InstanceIds: []string{instanceID},
	})
	return err
}

func StartInstance(ctx context.Context, client *ec2.Client, instanceID string) error {
	_, err := client.StartInstances(ctx, &ec2.StartInstancesInput{
		InstanceIds: []string{instanceID},
	})
	return err
}

func WaitForState(ctx context.Context, client *ec2.Client, instanceID, state string) error {
	switch state {
	case "stopped":
		waiter := ec2.NewInstanceStoppedWaiter(client)
		return waiter.Wait(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{instanceID},
		}, 10*time.Minute)
	case "running":
		waiter := ec2.NewInstanceRunningWaiter(client)
		return waiter.Wait(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{instanceID},
		}, 10*time.Minute)
	default:
		return fmt.Errorf("unsupported state: %s", state)
	}
}
