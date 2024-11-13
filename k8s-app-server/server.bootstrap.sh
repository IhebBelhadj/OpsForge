#!/bin/bash

# Install Docker
apt-get update

# Install Docker
sudo apt-get install -y apt-transport-https ca-certificates curl software-properties-common
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
sudo apt-get install -y docker-ce

# Add vagrant user to the docker group to avoid using 'sudo' with Docker
sudo usermod -aG docker vagrant

# Install Docker Compose
sudo curl -L "https://github.com/docker/compose/releases/download/1.29.2/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose

# Run Docker Compose to start the containers
# cd /vagrant
# docker-compose up -d
#
# Install Webhook
sudo apt-get install -y webhook
# Copy the webhook configuration file
cat /vagrant/conf/webhook.conf.json >/etc/webhook.conf.json

sudo webhook -hooks /etc/webhook.conf.json -port 9000 &
echo "Provisioning complete! Webhook listening on port 9000"
