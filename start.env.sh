#!/bin/bash

# Start all specified Docker containers by name
docker start mysql || echo "Container mysql not found or already running."
docker start grafana || echo "Container grafana not found or already running."
docker start prometheus || echo "Container prometheus not found or already running."
docker start sonarqube || echo "Container sonarqube not found or already running."
docker start nexus || echo "Container nexus not found or already running."

echo "All containers attempted to start."
