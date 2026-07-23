package awsprovider

import (
	"testing"

	awsutil "github.com/cruxdigital-llc/conga-line/pkg/aws"
)

// TestMCPFileExists guards the AWS existence-probe semantics. The critical case
// is a missing file: RunCommand returns (result, nil) with Status "Failed" and
// empty stdout — mcpFileExists MUST report false so common.RestoreMCPOAuth writes
// the blob. Reading err instead of stdout would make this always-true and
// silently disable AWS restore.
func TestMCPFileExists(t *testing.T) {
	cases := []struct {
		name string
		res  *awsutil.RunCommandResult
		want bool
	}{
		{"present: success + marker", &awsutil.RunCommandResult{Status: "Success", Stdout: mcpOAuthExistsMarker}, true},
		{"missing: failed + empty stdout", &awsutil.RunCommandResult{Status: "Failed", Stdout: ""}, false},
		{"no marker in stdout", &awsutil.RunCommandResult{Status: "Success", Stdout: "some other output"}, false},
		{"nil result", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpFileExists(tc.res); got != tc.want {
				t.Errorf("mcpFileExists(%+v) = %v, want %v", tc.res, got, tc.want)
			}
		})
	}
}
