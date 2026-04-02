package terraform_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccChannelResource_lifecycle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: testAccChannelConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_channel.test", "platform", "slack"),
					resource.TestCheckResourceAttrSet("conga_channel.test", "id"),
				),
			},
			// Import
			{
				ResourceName:            "conga_channel.test",
				ImportState:             true,
				ImportStateId:           "slack",
				ImportStateVerifyIgnore: []string{"bot_token", "signing_secret", "app_token"},
			},
		},
	})
}

func TestAccChannelBindingResource_lifecycle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: testAccChannelBindingConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_channel_binding.test", "agent", "testagent"),
					resource.TestCheckResourceAttr("conga_channel_binding.test", "platform", "slack"),
					resource.TestCheckResourceAttr("conga_channel_binding.test", "binding_id", "U0TESTUSER"),
					resource.TestCheckResourceAttr("conga_channel_binding.test", "id", "testagent/slack"),
				),
			},
			// Import
			{
				ResourceName:            "conga_channel_binding.test",
				ImportState:             true,
				ImportStateId:           "testagent/slack",
				ImportStateVerifyIgnore: []string{"binding_id", "label"},
			},
		},
	})
}

func testAccChannelConfig() string {
	return `
provider "conga" {
  provider_type = "local"
}

resource "conga_environment" "test" {
  image = "ghcr.io/openclaw/openclaw:2026.3.11"
}

resource "conga_channel" "test" {
  platform       = "slack"
  bot_token      = "xoxb-test-bot-token"
  signing_secret = "test-signing-secret"
  app_token      = "xapp-test-app-token"
  depends_on     = [conga_environment.test]
}
`
}

func testAccChannelBindingConfig() string {
	return `
provider "conga" {
  provider_type = "local"
}

resource "conga_environment" "test" {
  image = "ghcr.io/openclaw/openclaw:2026.3.11"
}

resource "conga_agent" "test" {
  name       = "testagent"
  type       = "user"
  depends_on = [conga_environment.test]
}

resource "conga_channel" "test" {
  platform       = "slack"
  bot_token      = "xoxb-test-bot-token"
  signing_secret = "test-signing-secret"
  app_token      = "xapp-test-app-token"
  depends_on     = [conga_environment.test]
}

resource "conga_channel_binding" "test" {
  agent      = conga_agent.test.name
  platform   = conga_channel.test.platform
  binding_id = "U0TESTUSER"
}
`
}
