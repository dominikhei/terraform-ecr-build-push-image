package internals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

/* 
Helper functions for executing AWS / Docker operations using the AWS SDK and docker cli
*/

// Create a new ECR client with the given region
func getECRClient(ctx context.Context, region string) (*ecr.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, 
		config.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config: %w", err)
	}

	return ecr.NewFromConfig(cfg), nil
}

// Create an STS client for account operations, used to retrieve the AWS AccountID
func getSTSClient(ctx context.Context) (*sts.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config: %w", err)
	}

	return sts.NewFromConfig(cfg), nil
}

// Function to get the image manifest from ECR.
func getImageManifest(repoName, imageTag, awsRegion string) (string, error) {
	ctx := context.TODO()
	client, err := getECRClient(ctx, awsRegion)
	if err != nil {
		return "", err
	}

	input := &ecr.BatchGetImageInput{
		RepositoryName: aws.String(repoName),
		ImageIds: []types.ImageIdentifier{
			{
				ImageTag: aws.String(imageTag),
			},
		},
	}

	result, err := client.BatchGetImage(ctx, input)
	if err != nil {
		return "", fmt.Errorf("error getting image manifest: %w", err)
	}

	if len(result.Images) == 0 {
		return "", fmt.Errorf("no image found with tag %s in repository %s", imageTag, repoName)
	}

	return *result.Images[0].ImageManifest, nil
}

// Function to update the image tag in ECR.
func updateImageTag(imageManifest, repoName, newImageTag, awsRegion string) error {
	ctx := context.TODO()
	client, err := getECRClient(ctx, awsRegion)
	if err != nil {
		return err
	}

	input := &ecr.PutImageInput{
		RepositoryName: aws.String(repoName),
		ImageManifest:  aws.String(imageManifest),
		ImageTag:       aws.String(newImageTag),
	}

	_, err = client.PutImage(ctx, input)
	if err != nil {
		return fmt.Errorf("error updating image tag: %w", err)
	}

	return nil
}

// Function to get the AWS account ID.
func getAWSAccountID() (string, error) {
	ctx := context.TODO()
	client, err := getSTSClient(ctx)
	if err != nil {
		return "", err
	}

	result, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("error getting caller identity: %w", err)
	}

	return *result.Account, nil
}

// Function executing the docker build command.
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
func pushDockerImage(ecrUriWithTag, awsRegion, ecrUri string) error {
	ctx := context.TODO()
	client, err := getECRClient(ctx, awsRegion)
	if err != nil {
		return err
	}
	authInput := &ecr.GetAuthorizationTokenInput{}
	authOutput, err := client.GetAuthorizationToken(ctx, authInput)
	if err != nil {
		return fmt.Errorf("error getting ECR authorization token: %w", err)
	}

	if len(authOutput.AuthorizationData) == 0 {
		return fmt.Errorf("no authorization data returned")
	}
	authToken := *authOutput.AuthorizationData[0].AuthorizationToken
	
	dockerLoginCmd := fmt.Sprintf("echo %s | base64 -d | cut -d: -f2 | docker login --username AWS --password-stdin %s", 
		authToken, ecrUri)
	login := exec.Command("bash", "-c", dockerLoginCmd)
	loginOut, err := login.CombinedOutput()
	if err != nil {
		fmt.Println(string(loginOut))
		return fmt.Errorf("error logging in to ECR: %w", err)
	}

	pushCmd := fmt.Sprintf("docker push %s", ecrUriWithTag)
	push := exec.Command("bash", "-c", pushCmd)
	pushOut, err := push.CombinedOutput()
	if err != nil {
		fmt.Println(string(pushOut))
		return fmt.Errorf("error pushing image: %w", err)
	}

	return nil
}

// Function to delete the image from ECR.
func deleteImage(repoName, imageTag, awsRegion string) error {
	ctx := context.TODO()
	client, err := getECRClient(ctx, awsRegion)
	if err != nil {
		return err
	}

	input := &ecr.BatchDeleteImageInput{
		RepositoryName: aws.String(repoName),
		ImageIds: []types.ImageIdentifier{
			{
				ImageTag: aws.String(imageTag),
			},
		},
	}

	_, err = client.BatchDeleteImage(ctx, input)
	if err != nil {
		return fmt.Errorf("error deleting image: %w", err)
	}

	return nil
}

// Funtion to check whether the repository exists in the specified region.
func repoExists(repoName, awsRegion string) (bool, error) {
	ctx := context.TODO()
	client, err := getECRClient(ctx, awsRegion)
	if err != nil {
		return false, err
	}

	input := &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{repoName},
	}

	_, err = client.DescribeRepositories(ctx, input)
	if err != nil {
		var notFoundErr *types.RepositoryNotFoundException
		if errors.As(err, &notFoundErr) {
			return false, nil
		}
		return false, fmt.Errorf("error checking repository existence: %w", err)
	}

	return true, nil
}

// Function to check whether the image tag exists in the specified repository .
func imageTagExist(imageTag, repoName, awsRegion string) (bool, error) {
	ctx := context.TODO()
	client, err := getECRClient(ctx, awsRegion)
	if err != nil {
		return false, err
	}

	input := &ecr.ListImagesInput{
		RepositoryName: aws.String(repoName),
		Filter: &types.ListImagesFilter{
			TagStatus: types.TagStatusTagged,
		},
	}

	result, err := client.ListImages(ctx, input)
	if err != nil {
		return false, fmt.Errorf("error listing images: %w", err)
	}

	for _, imageID := range result.ImageIds {
		if imageID.ImageTag != nil && *imageID.ImageTag == imageTag {
			return true, nil
		}
	}

	return false, nil
}

// Function checking the ECR repositories mutability settings.
func isMutable(repoName, awsRegion string) (bool, error) {
	ctx := context.TODO()
	client, err := getECRClient(ctx, awsRegion)
	if err != nil {
		return false, err
	}

	input := &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{repoName},
	}

	result, err := client.DescribeRepositories(ctx, input)
	if err != nil {
		return false, fmt.Errorf("error describing repository: %w", err)
	}

	if len(result.Repositories) == 0 {
		return false, fmt.Errorf("repository %s not found", repoName)
	}

	repo := result.Repositories[0]
	return repo.ImageTagMutability != types.ImageTagMutabilityImmutable, nil
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

// Function to calculate a hash value of the Dockerfile based on its content using the SHA256 algorithm.
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