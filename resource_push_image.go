package main 

import (
	"os"
	"os/exec"
	"fmt"
	"strings"
	"encoding/json"
	"errors"
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
)

//func check whether aws cli is installed 

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
		return diag.FromErr(fmt.Errorf("error checking whether Docker is running: %s", err))
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
	
	repoName := d.Get("ecr_repository_name").(string)
	imageTag := d.Get("image_tag").(string)
	awsRegion := d.Get("aws_-region").(string)
	var diags diag.Diagnostics

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
	return diags
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
		return diag.Errorf("the provided ECR repository does not exist")
	}
	d.Set("ecr_repository_name", repoName)


	tagExists, err := imageTagExist(imageTag, repoName, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving image tag: %s", err))
	}
	if !tagExists {
		d.SetId("") 
		return diag.Errorf("the tag does not exist in the ecr reposiory, deleting the ressource")
	}
	d.Set("image_tag", imageTag)


	imageManifest, err := getImageManifest(repoName, imageTag, awsRegion)
	if err != nil {
		return diag.FromErr(fmt.Errorf("error retrieving image manifest: %s", err))
	}
	d.SetId(imageManifest)

	return diags
}


func getImageManifest(repoName, imageTag, awsRegion string) (string, error) {

	digestCMD := fmt.Sprintf("aws ecr batch-get-image --repository-name %s --image-ids imageTag=%s --query 'images[].imageManifest' --output text --region %s", repoName, imageTag, awsRegion)
	digest := exec.Command("bash", "-c", digestCMD)
	out, err := digest.CombinedOutput() 
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func updateImageTag(imageManifest, repoName, newImageTag, awsRegion string) error {
	updateTagCMD := fmt.Sprintf("aws ecr put-image --repository-name %s --image-tag %s --image-manifest '%s' --region %s", repoName, newImageTag, imageManifest, awsRegion)
	updateTag := exec.Command("bash", "-c", updateTagCMD)
	_, err := updateTag.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

func getAWSAccountID() (string, error) {
	getAccountIdCMD := exec.Command("aws", "sts", "get-caller-identity", "--query", "Account", "--output", "text")
	accountId, err := getAccountIdCMD.CombinedOutput()
	if err != nil {
		return "", err
	}
	accountIdTrimmed := strings.TrimSpace(string(accountId))
	return accountIdTrimmed, nil
}

func buildDockerImage(imageNameAndTag, dockerfilePath string) error {
	dockerBuildImage := exec.Command("docker", "build", "-t", imageNameAndTag, dockerfilePath) 
	out, err := dockerBuildImage.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return err
	}
	return nil
}

func tagDockerImage(imageNameAndTag, ecrUriWithTag string) error {
	tagCmd := fmt.Sprintf("docker tag %s %s", imageNameAndTag, ecrUriWithTag)
	tag := exec.Command("bash", "-c", tagCmd)
	out, err := tag.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return err
	}
	return nil
}

func pushDockerImage(ecrUriWithTag, awsRegion, ecrUri string) error {
	dockerPushCmd := fmt.Sprintf("docker push %s", ecrUriWithTag)
	pushImage := exec.Command("bash", "-c", dockerPushCmd)
	authenticateCommand := exec.Command("bash", "-c", "aws ecr get-login-password --region " + awsRegion + " | docker login --username AWS --password-stdin " + ecrUri)
	var err error
	pushImage.Stdin, err = authenticateCommand.StdoutPipe()
	if err != nil {
		fmt.Println(pushImage.Stdin) 
		return err
	}
	pushImage.Stdout = os.Stdout

	errStart := pushImage.Start()
	errRun := authenticateCommand.Run()
	errWait := pushImage.Wait()
	if errStart != nil {
		fmt.Println(errStart)
		return errStart
	}
	if errRun != nil {
		fmt.Println(errRun)
		return errRun
	}
	if errWait != nil {
		fmt.Println(errWait)
		return errWait
	}
	return nil
}

func deleteImage(repoName, imageTag, awsRegion string) error {
	deleteCommand := fmt.Sprintf("aws ecr batch-delete-image --repository-name %s --image-ids imageTag=%s --output text --region %s", repoName, imageTag, awsRegion)
	deleteImage := exec.Command("bash", "-c", deleteCommand)
	out, err := deleteImage.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return err
	}
	return nil
}

func repoExists(repoName, awsRegion string) (bool, error) {
	describeReposCMD := fmt.Sprintf("aws ecr describe-repositories --query 'repositories[].repositoryName' --output json --region %s", awsRegion)
	decribeRepos := exec.Command("bash", "-c", describeReposCMD)
	out, err :=  decribeRepos.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return false, err
	}
	var repoNames []string
	if err := json.Unmarshal(out, &repoNames); err != nil {
		return false, err
	}
	for _, name := range repoNames {
		if name == repoName {
			return true, nil }
		}
	return false, errors.New("repository does not exist")
 }


 func imageTagExist(imageTag, repoName, awsRegion string) (bool, error) {
	listImagesCMD := fmt.Sprintf("aws ecr list-images --repository-name %s --query 'imageIds[].imageTag' --output json --region %s", repoName, awsRegion)
	listImages := exec.Command("bash", "-c", listImagesCMD)
	out, err := listImages.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return false, err
	}
	var imageTags []string
	if err := json.Unmarshal(out, &imageTags); err != nil {
		return false, err
	}
	for _, name := range imageTags {
		if name == imageTag {
			return true, nil }
		}
	return false, nil
 }

 func isMutable(repoName, awsRegion string) (bool, error) {
	tagMutabilityCMD := fmt.Sprintf("aws ecr describe-repositories --repository-names %s --query 'repositories[].imageTagMutability' --output json --region %s", repoName, awsRegion)
	tagMutability := exec.Command("bash", "-c", tagMutabilityCMD)
	out, err := tagMutability.CombinedOutput()
	if err != nil {
		return false, err
	}
	var response []string
	if err := json.Unmarshal(out, &response); err != nil {
		return false, err
	}
	for _, value := range response {
		if value == "IMMUTABLE" {
			return false, nil
		}
	}
	return true, nil
 }

 func isDockerDRunning() (bool, error) {

	dockerCheckCMD := "docker ps"
	isInstalled := exec.Command("bash", "-c", dockerCheckCMD)
	out, err := isInstalled.CombinedOutput()
	if err != nil {
		return false, err
	}
	if strings.Contains(string(out), "Cannot connect") {
		return false, nil
	}
	return true, nil
 }