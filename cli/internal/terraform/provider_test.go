package terraform_test

import (
	"github.com/cruxdigital-llc/conga-line/cli/internal/terraform"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"conga": providerserver.NewProtocol6WithError(terraform.New("test")()),
}
