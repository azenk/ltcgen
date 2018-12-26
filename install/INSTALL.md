# Installation Instructions

## Raspbian

1. Install raspbian-lite
1. Configure network
1. Configure ntpd
  1. `apt-get install ntp`
  1. `systemctl enable ntp`
1. Make binary, copy to the pi
1. Create systemd.service file
1. Enable ltcgen service `systemctl enable ltcgen`
1. Set default audio output to analog output `raspi-config` > Advanced > Audio
1. Reboot
