package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type RunCommandResult struct {
	Status string
	Stdout string
	Stderr string
}

func RunCommand(ctx context.Context, client *ssm.Client, instanceID, script string, timeout time.Duration) (*RunCommandResult, error) {
	out, err := client.SendCommand(ctx, &ssm.SendCommandInput{
		InstanceIds:  []string{instanceID},
		DocumentName: aws.String("AWS-RunShellScript"),
		Parameters: map[string][]string{
			"commands": {script},
		},
		TimeoutSeconds: aws.Int32(int32(timeout.Seconds())),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	commandID := aws.ToString(out.Command.CommandId)
	deadline := time.After(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	consecutiveErrors := 0
	for {
		select {
		case <-deadline:
			return nil, fmt.Errorf("command timed out after %s", timeout)
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			inv, err := client.GetCommandInvocation(ctx, &ssm.GetCommandInvocationInput{
				CommandId:  aws.String(commandID),
				InstanceId: aws.String(instanceID),
			})
			if err != nil {
				consecutiveErrors++
				if consecutiveErrors > 5 {
					return nil, fmt.Errorf("failed to get command status after %d attempts: %w", consecutiveErrors, err)
				}
				continue
			}
			consecutiveErrors = 0

			status := string(inv.Status)
			switch status {
			case "Success", "Failed":
				return &RunCommandResult{
					Status: status,
					Stdout: aws.ToString(inv.StandardOutputContent),
					Stderr: aws.ToString(inv.StandardErrorContent),
				}, nil
			}
		}
	}
}
