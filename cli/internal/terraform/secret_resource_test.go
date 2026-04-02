package terraform_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccSecretResource_lifecycle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: testAccSecretConfig("test-secret-value-1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_secret.test", "agent", "testagent"),
					resource.TestCheckResourceAttr("conga_secret.test", "name", "anthropic-api-key"),
					resource.TestCheckResourceAttr("conga_secret.test", "id", "testagent/anthropic-api-key"),
				),
			},
			// Import (value cannot be imported)
			{
				ResourceName:            "conga_secret.test",
				ImportState:             true,
				ImportStateId:           "testagent/anthropic-api-key",
				ImportStateVerifyIgnore: []string{"value"},
			},
			// Update value
			{
				Config: testAccSecretConfig("test-secret-value-2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_secret.test", "agent", "testagent"),
					resource.TestCheckResourceAttr("conga_secret.test", "name", "anthropic-api-key"),
				),
			},
		},
	})
}

func testAccSecretConfig(value string) string {
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

resource "conga_secret" "test" {
  agent = conga_agent.test.name
  name  = "anthropic-api-key"
  value = "` + value + `"
}
`
}
