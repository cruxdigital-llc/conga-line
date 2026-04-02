# Local provider — Docker containers on your machine
provider "conga" {
  provider_type = "local"
}

# Remote provider — Docker on an SSH-accessible host
# provider "conga" {
#   provider_type = "remote"
#   ssh_host      = "vps.example.com"
#   ssh_user      = "root"
#   ssh_key_path  = "~/.ssh/id_ed25519"
# }

# AWS provider — EC2 host in a zero-ingress VPC
# provider "conga" {
#   provider_type = "aws"
#   region        = "us-east-2"
#   profile       = "myprofile"
# }
