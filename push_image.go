package main 

import (
	"os"
	"os/exec"
	"fmt"
	"strings"
	"encoding/json"
	"log"
	"errors"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
)

func ResourcePushImage() *schema.Resource {
	return &schema.Resource{
		Create: resourcePushImageCreate,
		Delete: resourcePushImageDelete,
		Update: resourcePushImageUpdate,
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


func resourcePushImageCreate(d *schema.ResourceData, meta interface{}) error {
	
	awsRegion := d.Get("aws_region").(string)
	repoName := d.Get("ecr_repository_name").(string)
	imageName := d.Get("image_name").(string)
	imageTag := d.Get("image_tag").(string)
	dockerfilePath := d.Get("dockerfile_path").(string)
	imageNameAndTag := fmt.Sprintf("%s:%s", imageName, imageTag)

	out, err := repoExists(repoName, awsRegion)
	if err != nil {
		log.Fatal(err)
	}
	if out != true {
		log.Fatal("The provided ECR repository does not exist")
	}

	repoMutability, err := isMutable(repoName, awsRegion)
	if err != nil {
		log.Fatal(err)
	}
	tagAlreadyExists, err := imageTagExist(imageTag, repoName, awsRegion) 
	if err != nil {
		log.Fatal(err)
	}

	if tagAlreadyExists == true && repoMutability == false {
		log.Fatal("The repo is immutable and you are trying to push an image with a tag that already exists in it")
	}

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
		log.Fatal("Error tagging Docker image: ", err)		
	}
	fmt.Println("Pushing Docker image")
	err = pushDockerImage(ecrUriWithTag, awsRegion, ecrUri)
	if err != nil {
		log.Fatal("Error pushing Docker image: ", err)		
	}
	fmt.Println("Docker image successfully pushed to ECR")

	return nil
}


func resourcePushImageDelete(d *schema.ResourceData, meta interface{}) error { 
	
	repoName := d.Get("ecr_repository_name").(string)
	imageTag := d.Get("image_tag").(string)
	awsRegion := d.Get("aws_-region").(string)

	out, err := repoExists(repoName, awsRegion)
	if err != nil {
		log.Fatal(err)
	}
	if out != true {
		log.Fatal("The provided ECR repository does not exist")
	}

	out, err = imageTagExist(imageTag, repoName, awsRegion)
	if err != nil {
		log.Fatal(err)
	}
	if out != true {
		log.Fatal("The provided Image tag does not exist in the repository")
	}

	fmt.Println("Deleting image")
	err = deleteImage(repoName, imageTag, awsRegion)
	if err != nil {
		log.Fatal("Error deleting Image", err)
	}
	fmt.Println("Docker image successfully removed from ECR")

	return nil
}

func resourcePushImageUpdate(d *schema.ResourceData, meta interface{}) error {
	if d.HasChange("image_tag") {
		repoName := d.Get("ecr_repository_name").(string)
		oldVal, newVal := d.GetChange("image_tag")
		oldTag := oldVal.(string)
		newTag := newVal.(string)
		awsRegion := d.Get("aws_region").(string)

		out, err := repoExists(repoName, awsRegion)
		if err != nil {
			log.Fatal(err)
		}
		if out != true {
			log.Fatal("The provided ECR repository does not exist")
		}
	
		out, err = imageTagExist(oldTag, repoName, awsRegion)
		if err != nil {
			log.Fatal(err)
		}
		if out != true {
			log.Fatal("The previous Image tag does not exist anymore in the repository")
		}
	
		repoMutability, err := isMutable(repoName, awsRegion)
		if err != nil {
			log.Fatal(err)
		}
		newTagAlreadyExists, err := imageTagExist(newTag, repoName, awsRegion) 
		if err != nil {
			log.Fatal(err)
		}
	
		if newTagAlreadyExists == true && repoMutability == false {
			log.Fatal("The repositorie is immutable and you are trying to update an image with a tag that already exists in the repositorie")
		}

		imageManifest, err := getImageManifest(repoName, oldTag, awsRegion)
		if err != nil {
			log.Fatal("Error retriving Image digest", err)
		}
		err = updateImageTag(imageManifest, repoName, newTag, awsRegion)
		if err != nil {
			log.Fatal("Error updating Image Tag", err)
		}
		err = deleteImage(repoName, oldTag, awsRegion)
		if err != nil {
			log.Fatal("Error deleting the old image tag")
		}
	}
	return nil
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
	return false, errors.New("Repository does not exist")
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