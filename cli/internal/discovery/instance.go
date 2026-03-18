package discovery

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
)

var (
	cachedInstanceID string
	instanceOnce     sync.Once
	instanceErr      error
)

func FindInstance(ctx context.Context, ec2Client *ec2.Client, tag string) (string, error) {
	instanceOnce.Do(func() {
		cachedInstanceID, instanceErr = awsutil.FindInstanceByTag(ctx, ec2Client, tag)
	})
	return cachedInstanceID, instanceErr
}
