package terraform_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccPolicyResource_lifecycle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with egress
			{
				Config: testAccPolicyConfig("enforce"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_policy.test", "id", "policy"),
					resource.TestCheckResourceAttr("conga_policy.test", "egress_mode", "enforce"),
					resource.TestCheckResourceAttr("conga_policy.test", "egress_allowed_domains.#", "2"),
					resource.TestCheckResourceAttr("conga_policy.test", "egress_allowed_domains.0", "api.anthropic.com"),
					resource.TestCheckResourceAttr("conga_policy.test", "egress_allowed_domains.1", "*.slack.com"),
				),
			},
			// Import
			{
				ResourceName:  "conga_policy.test",
				ImportState:   true,
				ImportStateId: "policy",
			},
			// Update to validate mode
			{
				Config: testAccPolicyConfig("validate"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_policy.test", "egress_mode", "validate"),
				),
			},
		},
	})
}

func testAccPolicyConfig(mode string) string {
	return `
provider "conga" {
  provider_type = "local"
}

resource "conga_environment" "test" {
  image = "ghcr.io/openclaw/openclaw:2026.3.11"
}

resource "conga_policy" "test" {
  egress_mode            = "` + mode + `"
  egress_allowed_domains = ["api.anthropic.com", "*.slack.com"]
  depends_on             = [conga_environment.test]
}
`
}
