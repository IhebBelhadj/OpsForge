# -*- mode: ruby -*-
# vi: set ft=ruby :

# All Vagrant configuration is done below. The "2" in Vagrant.configure
# configures the configuration version (we support older styles for
# backwards compatibility). Please don't change it unless you know what
# you're doing.
Vagrant.configure("2") do |config|
  # boxes at https://vagrantcloud.com/search.
  config.vm.box = "bento/ubuntu-22.04"

  # Network configuration (optional)
  config.vm.network "private_network", ip: "192.168.56.10"

  # VM settings
  config.vm.provider "virtualbox" do |vb|
    vb.memory = "7048"
    vb.cpus = 5
  end

  # Provisioning
  config.vm.provision "shell", path: "bootstrap.sh"

end
