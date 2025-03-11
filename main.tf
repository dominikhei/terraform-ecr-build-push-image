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

resource "aws_ecr_push_image" "example" {
  provider = "ecrbuildpush"
  ecr_repository_name = "provider-test-repo"    
  dockerfile_path     = "."     
  image_name          = "promtail"          
  image_tag           = "v22"                 
}