package main 

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