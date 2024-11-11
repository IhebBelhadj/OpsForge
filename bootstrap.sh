#!/bin/bash

# Update the package list
sudo apt-get update

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

# Install Jenkins
curl -fsSL https://pkg.jenkins.io/debian/jenkins.io.key | sudo tee /usr/share/keyrings/jenkins-keyring.asc >/dev/null
echo "deb [signed-by=/usr/share/keyrings/jenkins-keyring.asc] https://pkg.jenkins.io/debian binary/" | sudo tee /etc/apt/sources.list.d/jenkins.list >/dev/null

sudo apt-get install -y openjdk-17-jdk jenkins

# Install Zsh
sudo apt-get install -y zsh

# Make Zsh the default shell for the vagrant user
chsh -s $(which zsh) vagrant

# Install Oh My Zsh for the vagrant user
sudo -u vagrant sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" || true

echo "ZSH_THEME='robbyrussell'" >>/home/vagrant/.zshrc
echo "plugins=(git docker)" >>/home/vagrant/.zshrc

# Install Neovim
sudo apt-get install -y neovim

# 2. Backup any existing Neovim config (if exists)
if [ -d "/home/vagrant/.config/nvim" ]; then
	echo "Backing up existing Neovim configuration..."
	mv /home/vagrant/.config/nvim /home/vagrant/.config/nvim.bak
fi

# 3. Clone AstroVim repository
git clone --depth 1 https://github.com/AstroNvim/template /home/vagrant/.config/nvim

# 4. Set correct permissions for the vagrant user
sudo chown -R vagrant:vagrant /home/vagrant/.config/nvim

# 5. Install the necessary plugins with Neovim
sudo -u vagrant nvim +PackerSync +qall

echo "Neovim and AstroVim setup completed."

# Install Ansible
sudo apt-get install -y ansible

# Install Python3-pip for Docker SDK in Ansible
sudo apt-get install -y python3-pip

# Run the Ansible playbook
# I added the -VVV flag to increase verbosity for debugging purposes
ansible-playbook -i /vagrant/ansible/hosts /vagrant/ansible/docker_containers.yml -vvv

# Install maven for Jenkins
sudo apt-get install -y maven
# add docker permissions to jenkins
sudo usermod -aG docker jenkins
# Start Jenkins service and enable it to start at boot
sudo systemctl start jenkins
sudo systemctl enable jenkins

echo "Waiting for Jenkins to initialize..."
sleep 30

#Output the initial admin password for Jenkins setup
echo "Jenkins initial admin password:"
sudo cat /var/lib/jenkins/secrets/initialAdminPassword
