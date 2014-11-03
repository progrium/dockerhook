# dockerhook

Daemon that listens for Docker events and triggers a hook script for each event, passing any container data to STDIN.

## Notes

Currently using a patched version of Docker client, so building may fail for you. There is a binary release available for now.