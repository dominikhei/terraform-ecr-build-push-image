package internals 

import (
	"os"
	"os/exec"
	"fmt"
	"strings"
	"encoding/json"
	"errors"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

/*
This file contains helper functions, executing the relevant CLI commands for the providers functionality.
*/

// Function to get the image manifest from ECR.
// Requires the name of the repository, tag of the image and its AWS region as inputs.
func getImageManifest(repoName, imageTag, awsRegion string) (string, error) {
	digestCMD := fmt.Sprintf("aws ecr batch-get-image --repository-name %s --image-ids imageTag=%s --query 'images[].imageManifest' --output text --region %s", repoName, imageTag, awsRegion)
	digest := exec.Command("bash", "-c", digestCMD)
	out, err := digest.CombinedOutput() 
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Function to update the image tag in ECR.
// Requires the mainfest of the image from ECR, the name of the repository, the new tag of the image, that should replkace the old one and its AWS region as inputs.
func updateImageTag(imageManifest, repoName, newImageTag, awsRegion string) error {
	updateTagCMD := fmt.Sprintf("aws ecr put-image --repository-name %s --image-tag %s --image-manifest '%s' --region %s", repoName, newImageTag, imageManifest, awsRegion)
	updateTag := exec.Command("bash", "-c", updateTagCMD)
	_, err := updateTag.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

// Function to get the AWS account ID.
func getAWSAccountID() (string, error) {
	getAccountIdCMD := exec.Command("aws", "sts", "get-caller-identity", "--query", "Account", "--output", "text")
	accountId, err := getAccountIdCMD.CombinedOutput()
	if err != nil {
		return "", err
	}
	accountIdTrimmed := strings.TrimSpace(string(accountId))
	return accountIdTrimmed, nil
}

// Function executing the docker build command.
// Recquiring the imageName:tag and the path to the Dockerfile as input.
func buildDockerImage(imageNameAndTag, dockerfilePath string) error {
	dockerBuildImage := exec.Command("docker", "build", "-t", imageNameAndTag, dockerfilePath) 
	out, err := dockerBuildImage.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return err
	}
	return nil
}

// Function to tag the local image.
// Requires imageName:tag and the ECR uri with a tag appendend as inputs.
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

// Function to push the image to ECR.
// Requires the ECR Uri with the image tag appended, the region of the repository and the ECR Uri without tag appended as inputs.
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

// Function to delete the image from ECR.
// Requires the repositories name, image tag and AWS region of the repository as inputs.
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

// Function to check whether the repository exists in the specified AWS region.
// Requires the repositories name and its region as inputs.
func repoExists(repoName, awsRegion string) (bool, error) {
	describeReposCMD := fmt.Sprintf("aws ecr describe-repositories --query 'repositories[].repositoryName' --output json --region %s", awsRegion)
	decribeRepos := exec.Command("bash", "-c", describeReposCMD)
	out, err :=  decribeRepos.CombinedOutput()
	if err != nil {
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

// Function to check whether the image tag exists in ECR.
// Requires the image tag, the name of the repsotiry in which to look for and its AWS region as inputs.
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

 // Function checking the ECR repositories mutability settings.
 // Recquires the name of the repository and its AWS region as inputs. 
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

// Function checking whether the Docker daemon is running.
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

// Function to calculate a hash value of the Dockerfile based on its content using a SHA256 algorithm.
// It is used to detect changes within a Dockerfile in a simple manner, leading to a rebuilt.
 func getDockerfileHash(dockerfilePath string) (string, error) {
	fullPath := filepath.Join(dockerfilePath, "Dockerfile")
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	hash.Write(content)
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes), nil
}