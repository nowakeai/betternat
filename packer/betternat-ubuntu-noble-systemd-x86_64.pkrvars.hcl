flavor           = "ubuntu-noble-systemd"
architecture     = "x86_64"
base_image_name  = "ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"
base_image_owner = "099720109477"
ssh_username     = "ubuntu"
loxilb_runtime   = "systemd"
provision_script = "scripts/ami/provision-betternat-ami-ubuntu-systemd.sh"

# LoxiLB currently publishes amd64 .deb release assets. Keep this flavor
# explicit until an arm64 .deb or BetterNAT-owned package is available.
loxilb_deb_url    = "https://github.com/loxilb-io/loxilb/releases/download/v0.9.8/loxilb_0.9.8-amd64.deb"
loxilb_deb_sha256 = "b1f478356f7eee4140cbc5e7e970ca443ca6acca04161ed065a0eee7251c6290"
