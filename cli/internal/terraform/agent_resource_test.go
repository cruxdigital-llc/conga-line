package terraform_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAgentResource_lifecycle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create user agent
			{
				Config: testAccAgentConfig("testagent", "user"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_agent.test", "name", "testagent"),
					resource.TestCheckResourceAttr("conga_agent.test", "type", "user"),
					resource.TestCheckResourceAttrSet("conga_agent.test", "id"),
					resource.TestCheckResourceAttrSet("conga_agent.test", "gateway_port"),
					resource.TestCheckResourceAttr("conga_agent.test", "paused", "false"),
				),
			},
			// Import
			{
				ResourceName:      "conga_agent.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "testagent",
			},
			// Type change forces recreate
			{
				Config: testAccAgentConfig("testagent", "team"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_agent.test", "type", "team"),
				),
			},
		},
	})
}

func testAccAgentConfig(name, agentType string) string {
	return `
provider "conga" {
  provider_type = "local"
}

resource "conga_environment" "test" {
  image = "ghcr.io/openclaw/openclaw:2026.3.11"
}

resource "conga_agent" "test" {
  name       = "` + name + `"
  type       = "` + agentType + `"
  depends_on = [conga_environment.test]
}
`
}
