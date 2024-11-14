#!/bin/bash
echo "$(date) - Received request with token: $1" >>/var/log/webhook.log
sudo chmod +x /usr/local/bin/docker-compose

cd /vagrant
# docker-compose down >>/var/log/webhook.log 2>&1
# docker-compose pull >>/var/log/webhook.log 2>&1
# docker-compose up -d >>/var/log/webhook.log 2>&1
#
echo "Pulling app..."
echo "$(date) - Webhook execution completed" >>/var/log/webhook.log
