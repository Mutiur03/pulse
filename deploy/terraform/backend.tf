# Keep state remote and encrypted: it holds generated passwords.
# terraform {
#   backend "s3" {
#     bucket       = "my-terraform-state"
#     key          = "pulse/terraform.tfstate"
#     region       = "us-east-1"
#     encrypt      = true
#     use_lockfile = true
#   }
# }
