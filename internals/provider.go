package internals

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"aws_region": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The AWS region in which the ECR repsotiry is located",
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"ecrbuildpush_aws_ecr_push_image": ResourcePushImage(),
		},
		ConfigureContextFunc: providerConfigure,
	}
}

func providerConfigure(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	var diags diag.Diagnostics
	awsRegion, ok := d.GetOk("aws_region")
	if !ok {
		return nil, diag.Errorf("aws_region is required")
	}

	return awsRegion.(string), diags
}
