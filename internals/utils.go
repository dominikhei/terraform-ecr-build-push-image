package internals

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/jsonmessage"
)

/*
Helper functions for executing AWS / Docker operations using the AWS SDK and Moby Docker client.
*/

// Create a new ECR client with the given region.
func getECRClient(ctx context.Context, region string) (*ecr.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config: %w", err)
	}

	return ecr.NewFromConfig(cfg), nil
}

// Create an STS client for account operations, used to retrieve the AWS AccountID.
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
		ImageIds: []ecrtypes.ImageIdentifier{
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

// Function returning a Docker client.
func getDockerClient() (*client.Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}
	return cli, nil
}

// Function executing the docker build command using Moby.
func buildDockerImage(imageNameAndTag, dockerfilePath string) error {
	ctx := context.Background()
	cli, err := getDockerClient()
	if err != nil {
		return err
	}
	defer cli.Close()

	buildContext, err := archive.TarWithOptions(dockerfilePath, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("error creating build context: %w", err)
	}
	defer buildContext.Close()
	buildOptions := types.ImageBuildOptions{
		Tags:       []string{imageNameAndTag},
		Dockerfile: "Dockerfile",
		Remove:     true,
	}
	resp, err := cli.ImageBuild(ctx, buildContext, buildOptions)
	if err != nil {
		return fmt.Errorf("error building Docker image: %w", err)
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	for {
		var msg jsonmessage.JSONMessage
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error decoding build response: %w", err)
		}

		if msg.Error != nil {
			return errors.New(msg.Error.Message)
		}

		if msg.Stream != "" {
			fmt.Print(msg.Stream)
		}
	}

	return nil
}

// Function to tag the local image using Moby.
func tagDockerImage(imageNameAndTag, ecrUriWithTag string) error {
	ctx := context.Background()
	cli, err := getDockerClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	return cli.ImageTag(ctx, imageNameAndTag, ecrUriWithTag)
}

// Function to push the image to ECR using Moby.
func pushDockerImage(ecrUriWithTag, awsRegion, ecrUri string) error {
	ctx := context.Background()

	ecrClient, err := getECRClient(ctx, awsRegion)
	if err != nil {
		return err
	}

	authInput := &ecr.GetAuthorizationTokenInput{}
	authOutput, err := ecrClient.GetAuthorizationToken(ctx, authInput)
	if err != nil {
		return fmt.Errorf("error getting ECR authorization token: %w", err)
	}
	if len(authOutput.AuthorizationData) == 0 {
		return fmt.Errorf("no authorization data returned")
	}

	authToken := *authOutput.AuthorizationData[0].AuthorizationToken
	decodedToken, err := base64.StdEncoding.DecodeString(authToken)
	if err != nil {
		return fmt.Errorf("error decoding authorization token: %w", err)
	}

	tokenParts := strings.SplitN(string(decodedToken), ":", 2)
	if len(tokenParts) != 2 {
		return fmt.Errorf("invalid authorization token format")
	}
	username := tokenParts[0]
	password := tokenParts[1]

	cli, err := getDockerClient()
	if err != nil {
		return err
	}
	defer cli.Close()

	authConfig := registry.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: strings.TrimPrefix(ecrUri, "https://"),
	}
	encodedAuth, err := json.Marshal(authConfig)
	if err != nil {
		return fmt.Errorf("error encoding auth config: %w", err)
	}

	opts := image.PushOptions{
		RegistryAuth: base64.StdEncoding.EncodeToString(encodedAuth),
	}

	pushResp, err := cli.ImagePush(ctx, ecrUriWithTag, opts)
	if err != nil {
		return fmt.Errorf("error pushing image: %w", err)
	}
	defer pushResp.Close()

	dec := json.NewDecoder(pushResp)
	for {
		var msg jsonmessage.JSONMessage
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error decoding push response: %w", err)
		}
		if msg.Error != nil {
			return errors.New(msg.Error.Message)
		}
		if msg.Progress != nil || msg.Status != "" {
			if msg.Status != "" {
				fmt.Printf("%s\n", msg.Status)
			}
		}
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
		ImageIds: []ecrtypes.ImageIdentifier{
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

// Function to check whether the repository exists in the specified region.
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
		var notFoundErr *ecrtypes.RepositoryNotFoundException
		if errors.As(err, &notFoundErr) {
			return false, nil
		}
		return false, fmt.Errorf("error checking repository existence: %w", err)
	}
	return true, nil
}

// Function to check whether the image tag exists in the specified repository.
func imageTagExist(imageTag, repoName, awsRegion string) (bool, error) {
	ctx := context.TODO()
	client, err := getECRClient(ctx, awsRegion)
	if err != nil {
		return false, err
	}

	input := &ecr.ListImagesInput{
		RepositoryName: aws.String(repoName),
		Filter: &ecrtypes.ListImagesFilter{
			TagStatus: ecrtypes.TagStatusTagged,
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
	return repo.ImageTagMutability != ecrtypes.ImageTagMutabilityImmutable, nil
}

// Function checking whether the Docker daemon is running using Moby.
func isDockerDRunning() (bool, error) {
	ctx := context.Background()
	cli, err := getDockerClient()
	if err != nil {
		return false, err
	}
	defer cli.Close()

	_, err = cli.Ping(ctx)
	if err != nil {
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
