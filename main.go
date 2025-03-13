package main 

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
	internals "terraform-provider-ecrpushimage/internals"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: internals.Provider,
	})
}