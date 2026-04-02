package main

import (
	"context"
	"flag"
	"log"

	"github.com/cruxdigital-llc/conga-line/cli/internal/terraform"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/cruxdigital-llc/conga",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), terraform.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
