package discovery

import (
	"context"
	"sync"

	awsutil "github.com/cruxdigital-llc/conga-line/cli/pkg/aws"
)

var (
	cachedInstanceID string
	instanceOnce     sync.Once
	instanceErr      error
)

func FindInstance(ctx context.Context, ec2Client awsutil.EC2Client, tag string) (string, error) {
	instanceOnce.Do(func() {
		cachedInstanceID, instanceErr = awsutil.FindInstanceByTag(ctx, ec2Client, tag)
	})
	return cachedInstanceID, instanceErr
}
