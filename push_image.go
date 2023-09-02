package main 

import (
	"os"
	"os/exec"
	"fmt"
	"strings"
	"encoding/json"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
)

func ResourcePushImage() *schema.Resource {
	return &schema.Resource{
		Create: resourcePushImageCreate,
		Delete: resourcePushImageDelete,
		Schema: map[string]*schema.Schema{
				"ecr_repository_name": {
					Type:        schema.TypeString,
					Required:    true,
				},
				"dockerfile_path": {
					Type:        schema.TypeString,
					Required:    false,
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


func resourcePushImageCreate(d *schema.ResourceData) {
	
	awsRegion := d.Get("aws_region").(string)
	repoName := d.Get("ecr_repository_name").(string)
	imageName := d.Get("image_name").(string)
	imageTag := d.Get("image_tag").(string)
	dockerfilePath := d.Get("dockerfile_path").(string)

	imageNameAndTag := fmt.Sprintf("%s:%s", imageName, imageTag)

	fmt.Println("Retrieving AWS account Id")
	awsAccountId, err := getAWSAccountID()
	if err != nil {
		log.Fatal("Error retrieving AWS account Id: ", err)
	}
	ecrUri := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", awsAccountId, awsRegion)
	ecrUriWithRepo := fmt.Sprintf("%s/%s", ecrUri, repoName)
	ecrUriWithTag := fmt.Sprintf("%s:%s", ecrUriWithRepo, imageTag)

	fmt.Println("Building Docker image: ", imageName)
	err = buildDockerImage(imageNameAndTag, dockerfilePath)
	if err != nil {
		log.Fatal("Error building Docker image: ", err)		
	}
	fmt.Println("Tagging Docker image")
	err = tagDockerImage(imageNameAndTag, ecrUriWithTag)
	if err != nil {
		log.Fatal("Error tagging Docker image", err)		
	}
	fmt.Println("Pushing Docker image")
	err = pushDockerImage(ecrUriWithTag, awsRegion, ecrUri)
	if err != nil {
		log.Fatal("Error pushing Docker image", err)		
	}
	fmt.Println("Docker image successfully pushed to ECR")
}

func resourcePushImageDelete(d *schema.ResourceData) { 
	
	repoName := d.Get("ecr_repository_name").(string)
	imageTag := d.Get("image_tag").(string)

	fmt.Println("Retrieving image digest from ECR")
	imageDigest, err := getImageDigest(repoName, imageTag)
	if err != nil {
		log.Fatal("Error retriving Image digest", err)
	}

	fmt.Println("Deleting image")
	err = deleteImage(repoName, imageDigest)
	if err != nil {
		log.Fatal("Error deleting Image", err)
	}
	fmt.Println("Docker image successfully removed from ECR")
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
	out, err = tag.CombinedOutput()
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
		fmt.Println(pushImage.Stdin) //checken ob das geht
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

func getImageDigest(repoName, imageTag string) (string, error) {

	var imageData []struct {
		ImageDigest string `json:"imageDigest"`
		ImageTag    string `json:"imageTag"`
	}
	digestCommand := fmt.Sprintf("aws ecr list-images --repository-name %s --query \"imageIds[?imageTag=='%s']\" --output json", repoName, imageTag)
	getDigest := exec.Command("bash", "-c", digestCommand)
	out, err := getDigest.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return "", err
	}
	err = json.Unmarshal([]byte(string(out)), &imageData) 
	if err != nil {
		return "", err
		}
	imageDigest := imageData[0].ImageDigest

	return imageDigest, err
}

func deleteImage(repoName, imageDigest string) error {
	deleteCommand := fmt.Sprintf("aws ecr batch-delete-image --repository-name %s --image-ids imageDigest=%s --output text", repoName, imageDigest)
	deleteImage := exec.Command("bash", "-c", deleteCommand)
	out, err := deleteImage.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return err
	}
	return nil
}