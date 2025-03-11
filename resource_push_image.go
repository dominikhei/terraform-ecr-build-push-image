package main 

import (
	"fmt"
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
)

func ResourcePushImage() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourcePushImageCreate,
		DeleteContext: resourcePushImageDelete,
		UpdateContext: resourcePushImageUpdate,
		ReadContext: resourcePushImageRead,
		Schema: map[string]*schema.Schema{
				"ecr_repository_name": {
					Type:        schema.TypeString,
					Required:    true,
				},
				"dockerfile_path": {
					Type:        schema.TypeString,
					Optional:    true,
					Default:     ".",
				},
				"image_name": {
					Type: schema.TypeString,
					Required: true,
				},
				"image_tag": {
					Type: schema.TypeString,
					Required: true, 
				},

				"aws_region": {
					Type: schema.TypeString,
					Required: true,
				},
		},
	}
}


func resourcePushImageCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	
	awsRegion := d.Get("aws_region").(string)
	repoName := d.Get("ecr_repository_name").(string)
	imageName := d.Get("image_name").(string)
	imageTag := d.Get("image_tag").(string)
	dockerfilePath := d.Get("dockerfile_path").(string)
	imageNameAndTag := fmt.Sprintf("%s:%s", imageName, imageTag)
	var diags diag.Diagnostics

	dockerStatus, err := isDockerDRunning()
	if err != nil {
		return diag.FromErr(fmt.Errorf("the docker daemon is not running: %s", err))
	}
	if !dockerStatus {
		return diag.Errorf("the Docker daemon is not running, please start it before running terraform apply")
	}

	out, err := repoExists(repoName, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving repository: %s", err))
	}
	if !out {
		return diag.FromErr(fmt.Errorf("the provided repository does not exist: %s", err))
	}

	repoMutability, err := isMutable(repoName, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error regarding repository mutability: %s", err))
	}
	tagAlreadyExists, err := imageTagExist(imageTag, repoName, awsRegion) 
	if err != nil {
		return diag.FromErr(fmt.Errorf("error regarding image tag: %s", err))
	}

	if tagAlreadyExists && !repoMutability {
		return diag.FromErr(fmt.Errorf("the repo is immutable and you are trying to push an image with a tag that already exists in it: %s", err))
	}

	tflog.Info(ctx, "Retrieving AWS account Id")
	awsAccountId, err := getAWSAccountID()
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving AWS account Id: %s", err))
	}
	ecrUri := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", awsAccountId, awsRegion)
	ecrUriWithRepo := fmt.Sprintf("%s/%s", ecrUri, repoName)
	ecrUriWithTag := fmt.Sprintf("%s:%s", ecrUriWithRepo, imageTag)

	tflog.Info(ctx, fmt.Sprintf("Building Docker image: %s", imageName))
	err = buildDockerImage(imageNameAndTag, dockerfilePath)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error building Docker image: %s", err))
	}
	tflog.Info(ctx, "Tagging Docker image")
	err = tagDockerImage(imageNameAndTag, ecrUriWithTag)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error tagging Docker image: %s", err))
	}
	tflog.Info(ctx, "Pushing Docker image")

	err = pushDockerImage(ecrUriWithTag, awsRegion, ecrUri)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error pushing Docker image: %s", err))
	}
	tflog.Info(ctx, "Docker image successfully pushed to ECR")

	imageManifest, err := getImageManifest(repoName, imageTag, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving image manifest: %s", err))
	}
	d.SetId(imageManifest)
	return diags
}


func resourcePushImageDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics { 
	
	repoName, ok := d.Get("ecr_repository_name").(string)
	if !ok || repoName == "" {
		return diag.FromErr(fmt.Errorf("ecr_repository_name is not set"))
	}
	
	imageTag, ok := d.Get("image_tag").(string)
	if !ok || imageTag == "" {
		return diag.FromErr(fmt.Errorf("image_tag is not set"))
	}
	
	awsRegion, ok := d.Get("aws_region").(string)
	if !ok || awsRegion == "" {
		return diag.FromErr(fmt.Errorf("aws_region is not set"))
	}

	out, err := repoExists(repoName, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving repository: %s", err))
	}
	if !out {
		return diag.FromErr(fmt.Errorf("the provided ECR repository does not exist"))
	}

	out, err = imageTagExist(imageTag, repoName, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving image tag: %s", err))
	}
	if !out {
		return diag.FromErr(fmt.Errorf("the provided Image tag does not exist in the repository"))
	}

	tflog.Info(ctx, "Deleting image")
	err = deleteImage(repoName, imageTag, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error deleting image: %s", err))
	}
	tflog.Info(ctx, "Docker image successfully removed from ECR")
	
	d.SetId("")
	return diag.Diagnostics{}
}

func resourcePushImageUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	if d.HasChange("image_tag") {
		repoName := d.Get("ecr_repository_name").(string)
		oldVal, newVal := d.GetChange("image_tag")
		oldTag := oldVal.(string)
		newTag := newVal.(string)
		awsRegion := d.Get("aws_region").(string)

		out, err := repoExists(repoName, awsRegion)
		if err != nil {
			return diag.FromErr(fmt.Errorf("error retrieving the ECR repository: %s", err))
		}
		if !out {
			return diag.FromErr(fmt.Errorf("the provided ECR repository does not exist"))
		}
	
		out, err = imageTagExist(oldTag, repoName, awsRegion)
		if err != nil {
			return diag.FromErr(fmt.Errorf("error regarding image tag: %s", err))
		}
		if !out {
			return diag.FromErr(fmt.Errorf("the previous image tag does not exist anymore in the repository"))
		}
	
		repoMutability, err := isMutable(repoName, awsRegion)
		if err != nil {
			return diag.FromErr(fmt.Errorf("error regarding repository mutability: %s", err))
		}
		newTagAlreadyExists, err := imageTagExist(newTag, repoName, awsRegion) 
		if err != nil {
			return diag.FromErr(fmt.Errorf("error with updating the image tag: %s", err))
		}
	
		if newTagAlreadyExists && !repoMutability {
			return diag.FromErr(fmt.Errorf("the repositorie is immutable and you are trying to update an image with a tag that already exists in the repositorie"))
		}

		imageManifest, err := getImageManifest(repoName, oldTag, awsRegion)
		if err != nil {
			return diag.FromErr(fmt.Errorf("error retriving image digest: %s", err))
		}
		err = updateImageTag(imageManifest, repoName, newTag, awsRegion)
		if err != nil {
			return diag.FromErr(fmt.Errorf("error updating Image tag: %s", err))	
		}
		err = deleteImage(repoName, oldTag, awsRegion)
		if err != nil {
			return diag.FromErr(fmt.Errorf("error deleting the old image tag: %s", err))
		}
		tflog.Info(ctx, "Docker image successfully updated")
		d.SetId(imageManifest)
		return diags
		
	}
	tflog.Info(ctx, "No updates")
	return diags
}

func resourcePushImageRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	awsRegion := d.Get("aws_region").(string)
	repoName := d.Get("ecr_repository_name").(string)
	imageTag := d.Get("image_tag").(string)

	out, err := repoExists(repoName, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving the ECR repository: %s", err))
	}
	if !out {
		d.SetId("")
		return nil
	}
	d.Set("ecr_repository_name", repoName)


	tagExists, err := imageTagExist(imageTag, repoName, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving image tag: %s", err))
	}
	if !tagExists {
		d.SetId("") 
		return nil
	}
	d.Set("image_tag", imageTag)


	imageManifest, err := getImageManifest(repoName, imageTag, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving image manifest: %s", err))
	}
	d.SetId(imageManifest)

	return diags
}