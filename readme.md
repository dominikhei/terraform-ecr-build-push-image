# Terraform ECR Image Provider

This Terraform provider allows you to build, tag, push, and manage Docker images in Amazon ECR (Elastic Container Registry).

## Features

- Build Docker images from a local Dockerfile
- Push images to Amazon ECR repositories
- Manage image tags
- Automatically rebuild and update images when Dockerfile changes
- Delete images when resources are destroyed
- Respect ECR repository mutability settings

## Requirements

- Docker daemon must be installed and running on the machine 
- AWS cli with credentials configured to access ECR
- Terraform v0.14.0 or later

## Usage

### Provider Configuration

```hcl
terraform {
    required_providers {
        ecrbuildpush = {
            source = "dominikhei/ecrbuildpush"
            version = "= 1.0.0"
        }
    }
}

provider "ecrbuildpush" {
  aws_region = "eu-central-1"
}

resource "ecrbuildpush_aws_ecr_push_image" "example" {
  ecr_repository_name = "provider-test-repo"    
  dockerfile_path     = "."     
  image_name          = "promtail"          
  image_tag           = "v21"                 
}
```


## Arguments Reference

The following arguments are supported:

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `ecr_repository_name` | String | Yes | The name of your ECR repository (must already exist) |
| `image_name` | String | Yes | The name of the Docker image |
| `image_tag` | String | Yes | The tag of the Docker image |
| `dockerfile_path` | String | No | The path to the directory containing the Dockerfile (default: ".") |

## Behavior

### Create

When you run `terraform apply`:

1. Validates that the Docker daemon is running
2. Checks if the specified ECR repository exists
3. Builds the Docker image from the specified Dockerfile path
4. Tags the image with the ECR repository URI and tag
5. Authenticates with ECR and pushes the image

### Update

The resource handles updates in the following scenarios:

1. **Image tag changes**: 
   - The provider creates a new tag for the existing image and removes the old tag

2. **Dockerfile changes**:
   - The provider detects changes in the Dockerfile using the hash
   - Rebuilds the image and pushes it to ECR with the same tag
3. **Dockerfile and tag changes**:
    - both tag and image itself will get updated

### Delete

When you run `terraform destroy` or remove the resource:

1. Deletes the image from ECR

## Notes

- The provider respects the mutability settings of the ECR repository. If the repository is immutable, it will fail when trying to push an image with a tag that already exists.
- The ECR repository must already exist before using this provider.
- The Dockerfile must be named "Dockerfile" 
- Changes to the Dockerfile contents will trigger a rebuild and push.

### Roadmap:

- Use Docker and AWS Sdk
- Build Tests 
- Refine error handling 

### Note:
This is a custom provider and in no way affiliated with Amazon Web Services or Docker.