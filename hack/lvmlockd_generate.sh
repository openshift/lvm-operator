#!/bin/bash

# Extract the last octet of the IP address
# Function to convert IP address to a single integer
ip_to_int() {
    local IFS=.
    read -r i1 i2 i3 i4 <<< "$1"
    echo $(( (i1 << 24) + (i2 << 16) + (i3 << 8) + i4 ))
}

# Get the IP address
IP_ADDRESS=$(hostname -I | awk '{print $1}')

# Convert the IP address to an integer
HOST_ID=$(ip_to_int "$IP_ADDRESS")

# Ensure the host_id is within the range 1-2000
HOST_ID=$((HOST_ID % 2000 + 1))

# Write the config file
CONFIG_FILE="/etc/lvm/lvmlocal.conf"
echo "using host id $HOST_ID based on $IP_ADDRESS"
echo "local {" > $CONFIG_FILE
echo "    # The lvmlockd sanlock host_id." >> $CONFIG_FILE
echo "    # This must be unique among all hosts, and must be between 1 and 2000." >> $CONFIG_FILE
echo "    host_id = $HOST_ID" >> $CONFIG_FILE
echo "}" >> $CONFIG_FILE
