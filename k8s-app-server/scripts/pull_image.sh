#!/bin/bash
echo "$(date) - Received request with token: $1" >>/var/log/webhook.log

cd /vagrant
docker-compose pull >>/var/log/webhook.log 2>&1
docker-compose up -d >>/var/log/webhook.log 2>&1

echo "$(date) - Webhook execution completed" >>/var/log/webhook.log
