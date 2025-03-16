package main

import (
	internals "terraform-provider-ecrpushimage/internals"

	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: internals.Provider,
	})
}
