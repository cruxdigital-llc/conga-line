package terraform_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccEnvironmentResource_lifecycle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: testAccEnvironmentConfig("ghcr.io/openclaw/openclaw:2026.3.11"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_environment.test", "image", "ghcr.io/openclaw/openclaw:2026.3.11"),
					resource.TestCheckResourceAttrSet("conga_environment.test", "id"),
				),
			},
			// Import
			{
				ResourceName:  "conga_environment.test",
				ImportState:   true,
				ImportStateId: "local",
			},
			// Update image
			{
				Config: testAccEnvironmentConfig("ghcr.io/openclaw/openclaw:latest"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conga_environment.test", "image", "ghcr.io/openclaw/openclaw:latest"),
				),
			},
		},
	})
}

func testAccEnvironmentConfig(image string) string {
	return `
provider "conga" {
  provider_type = "local"
}

resource "conga_environment" "test" {
  image = "` + image + `"
}
`
}
