# Project: OpsForge

### Overview
OpsForge is a local DevOps infrastructure setup designed to automate the build, test, and deployment processes for applications using Vagrant, Jenkins, and Kubernetes. This setup integrates CI/CD best practices, artifact management, and real-time monitoring for efficient deployment workflows and app server management.

### Features
- **Automated CI/CD Pipeline**: Triggered by Git webhooks, the pipeline automates building, testing, and deployment using Jenkins.
- **Kubernetes App Clusters**: Deployed on a Kubernetes cluster, app servers are updated dynamically to pull the latest Docker images.
- **Monitoring & Visualization**: Integrated with Prometheus and Grafana for real-time performance monitoring and visualization.
- **Artifact Management**: Uses Nexus for efficient artifact and dependency management.
- **Code Quality Analysis**: SonarQube integration for continuous code quality checks.

### Architecture
- **Vagrant**: Manages virtual machines for the CI/CD environment.
- **Jenkins**: Core CI/CD orchestrator, configured to automate builds and trigger deployments.
- **SonarQube**: Performs code quality analysis and enforces coding standards.
- **Nexus**: Serves as a local repository to cache dependencies and artifacts.
- **Kubernetes**: Manages clusters for app servers, enabling container orchestration and scalability.
- **Prometheus & Grafana**: Provides monitoring, logging, and visualization of resource metrics.

### Prerequisites
- **Vagrant** and **VirtualBox** installed on your local machine.
- **Docker** installed for building and managing Docker images.
- **Kubernetes** cluster configured within the VMs.

### Installation

1. **Clone the repository**:
   ```bash
   git clone https://github.com/IhebBelhadj/OpsForge.git

   ```
2. **Launch the VM cluster**
 ```bash
   cd OpsForge
   chmod +x init.sh
   ./init.sh
   ```

3. Acess services in VMs
> **_NOTE:_**  The IP addresses and ports  for the services
> CI/CD server IP : `192.168.56.10`
##### service ports table 
| Service               | Container Name    | Host Port | Container Port |
|-----------------------|-------------------|-----------|----------------|
| Nexus                 | nexus             | 8081      | 8081           |
| PostgreSQL (SonarQube)| db                | (No port exposed) | (No port exposed) |
| SonarQube             | sonarqube         | 9000      | 9000           |
| Prometheus            | prometheus        | 9090      | 9090           |
| Grafana               | grafana           | 3000      | 3000           |
| MySQL                 | mysql             | 3306      | 3306           |

> App server IP address: `192.168.56.20`

4. **Setup Jenkins pipeline**
* Access Jenkins at http://<Jenkins_VM_IP>:8080.
* Configure the Jenkinsfile to specify your repository and webhook triggers.
* Configure jenkins credentials

### Usage
* Push code to the Git repository to trigger the CI/CD pipeline.
* View live application logs and metrics through Grafana dashboards.

### Future improvements
* Add automated scaling policies in Kubernetes.
* Integrate additional security measures, such as TLS for network communication.
* Implement automated backups for Nexus repository and Jenkins configuration.
* Adding other agents to jenkins server
* Automating Jenkins setup on VM provision
* Making the infrastructure project agnostic so that you can you it with any application project you have 
