# Sentinel VM Configuration for HA spot instance recovery
# All resources are conditional on enable_sentinel + use_spot_instance
#
# The sentinel runs in a separate region (default us-west1) for free tier eligibility:
#   - e2-micro (2 vCPUs, 1 GB RAM)
#   - pd-standard boot disk up to 30 GB
#   - us-west1 / us-central1 / us-east1

locals {
  use_sentinel = var.enable_sentinel && var.use_spot_instance
}

# Sentinel gets its own static IP in its region (GCP static IPs are regional)
resource "google_compute_address" "sentinel_ip" {
  count  = local.use_sentinel ? 1 : 0
  name   = "${var.instance_name}-sentinel-ip"
  region = var.sentinel_region
}

# Sentinel VM — tiny always-on instance that owns the public static IP
resource "google_compute_instance" "sentinel" {
  count = local.use_sentinel ? 1 : 0

  name         = "${var.instance_name}-sentinel"
  machine_type = var.sentinel_machine_type
  zone         = var.sentinel_zone

  tags = ["containarium-sentinel"]

  # Always-on: standard provisioning, live-migrate on maintenance
  scheduling {
    preemptible                 = false
    automatic_restart           = true
    on_host_maintenance         = "MIGRATE"
    provisioning_model          = "STANDARD"
  }

  boot_disk {
    auto_delete = true
    initialize_params {
      image = var.os_image
      size  = var.sentinel_boot_disk_size
      type  = "pd-standard"
    }
  }

  network_interface {
    network = "default"

    access_config {
      # Sentinel owns the public static IP
      nat_ip = google_compute_address.sentinel_ip[0].address
    }
  }

  metadata = {
    ssh-keys = join("\n", [
      for user, key in var.admin_ssh_keys :
      "${user}:${key}"
    ])
    startup-script = templatefile("${path.module}/scripts/startup-sentinel.sh", {
      admin_users             = keys(var.admin_ssh_keys)
      containarium_binary_url = var.containarium_binary_url
      spot_vm_name            = var.instance_name
      zone                    = var.zone
      project_id              = var.project_id
    })
  }

  service_account {
    email  = var.service_account_email
    scopes = ["cloud-platform"]
  }

  labels = merge(
    var.labels,
    {
      component = "containarium"
      role      = "sentinel"
    }
  )

  allow_stopping_for_update = true

  lifecycle {
    ignore_changes = [
      metadata["ssh-keys"],
    ]
  }
}

# Firewall: allow SSH/HTTP/HTTPS from internet to sentinel
resource "google_compute_firewall" "sentinel_ingress" {
  count = local.use_sentinel ? 1 : 0

  name    = "${var.instance_name}-sentinel-ingress"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["22", "80", "443"]
  }

  source_ranges = var.allowed_ssh_sources
  target_tags   = ["containarium-sentinel"]

  description = "Allow SSH/HTTP/HTTPS to Containarium sentinel"
}

# Firewall: allow sentinel to reach spot VM on forwarded ports (internal network)
resource "google_compute_firewall" "sentinel_to_spot" {
  count = local.use_sentinel ? 1 : 0

  name    = "${var.instance_name}-sentinel-to-spot"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["22", "80", "443", "8080", "50051"]
  }

  source_tags = ["containarium-sentinel"]
  target_tags = ["containarium-spot-backend"]

  description = "Allow sentinel to forward traffic to spot backend"
}

# Firewall: allow spot VM to download binary from sentinel (internal only)
resource "google_compute_firewall" "spot_to_sentinel_binary" {
  count = local.use_sentinel ? 1 : 0

  name    = "${var.instance_name}-spot-to-sentinel-binary"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["8888"]
  }

  source_tags = ["containarium-spot-backend"]
  target_tags = ["containarium-sentinel"]

  description = "Allow spot VM to download containarium binary from sentinel"
}

# Firewall: allow SSH management on port 2222 (port 22 is DNAT'd to spot VM)
resource "google_compute_firewall" "sentinel_mgmt_ssh" {
  count = local.use_sentinel ? 1 : 0

  name    = "${var.instance_name}-sentinel-mgmt-ssh"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["2222"]
  }

  source_ranges = var.allowed_ssh_sources
  target_tags   = ["containarium-sentinel"]

  description = "Allow SSH management to sentinel on port 2222 (port 22 forwarded to spot VM)"
}

# Copy containarium binary to sentinel VM via SSH provisioner
# The sentinel owns the static IP, so SSH provisioner can reach it directly.
resource "null_resource" "copy_binary_to_sentinel" {
  count = local.use_sentinel && var.containarium_binary_url == "" && var.ssh_private_key_path != "" ? 1 : 0

  depends_on = [
    google_compute_instance.sentinel,
  ]

  connection {
    type        = "ssh"
    user        = keys(var.admin_ssh_keys)[0]
    host        = google_compute_address.sentinel_ip[0].address
    private_key = file(var.ssh_private_key_path)
  }

  provisioner "file" {
    source      = "${path.module}/../../bin/containarium-linux-amd64"
    destination = "/tmp/containarium"
  }

  provisioner "remote-exec" {
    inline = [
      "sudo mv /tmp/containarium /usr/local/bin/containarium",
      "sudo chmod +x /usr/local/bin/containarium",
      # Install and start the sentinel service
      "sudo /usr/local/bin/containarium sentinel service install --spot-vm ${var.instance_name} --zone ${var.zone} --project ${var.project_id}",
      "sleep 2",
      "sudo systemctl status containarium-sentinel --no-pager || true"
    ]
  }

  triggers = {
    instance_id = google_compute_instance.sentinel[0].id
    binary_hash = filemd5("${path.module}/../../bin/containarium-linux-amd64")
  }
}
